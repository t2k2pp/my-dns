// dns-server is the main DNS forwarder with local blocklist, caching,
// auto-learning, and a management HTTP API.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/miekg/dns"
	"github.com/t2k2pp/my-dns/internal/blocklist"
	"github.com/t2k2pp/my-dns/internal/cache"
	"github.com/t2k2pp/my-dns/internal/config"
	"github.com/t2k2pp/my-dns/internal/logger"
)

// Atomic query counters – read by the management API without locking.
var (
	cntTotal       int64
	cntLocalBlock  int64
	cntAutoLearned int64
	cntCached      int64
	cntForwarded   int64
	cntError       int64
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to YAML config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	bl := blocklist.New(cfg.BlocklistFile)
	if err := bl.Load(); err != nil {
		log.Printf("warn: initial blocklist load: %v", err)
	}
	log.Printf("blocklist: %d entries from %s", bl.Count(), cfg.BlocklistFile)

	c := cache.New(time.Duration(cfg.CacheTTL) * time.Second)

	ql, err := logger.New(cfg.LogFile, cfg.LogMaxBytes)
	if err != nil {
		log.Fatalf("logger: %v", err)
	}
	defer ql.Close()

	startTime := time.Now()

	// Management HTTP API (health, metrics, reload, cache flush).
	mgmtSrv := buildManagementServer(cfg.ManagementAddr, bl, c, &startTime)
	go func() {
		log.Printf("management API listening on http://%s", cfg.ManagementAddr)
		if err := mgmtSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("management API error: %v", err)
		}
	}()

	// Periodic blocklist reload on file change.
	if cfg.BlocklistReloadInterval > 0 {
		go func() {
			ticker := time.NewTicker(time.Duration(cfg.BlocklistReloadInterval) * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				reloaded, err := bl.ReloadIfChanged()
				if err != nil {
					log.Printf("blocklist reload error: %v", err)
				} else if reloaded {
					log.Printf("blocklist reloaded: %d entries", bl.Count())
				}
			}
		}()
	}

	// DNS handler.
	dnsClient := &dns.Client{
		Net:     "udp",
		Timeout: time.Duration(cfg.UpstreamTimeoutSec) * time.Second,
	}
	dns.HandleFunc(".", func(w dns.ResponseWriter, r *dns.Msg) {
		handleRequest(w, r, cfg, bl, c, ql, dnsClient)
	})

	dnsSrv := &dns.Server{Addr: cfg.ListenAddr(), Net: "udp"}
	go func() {
		log.Printf("DNS server listening on %s (UDP)", cfg.ListenAddr())
		if err := dnsSrv.ListenAndServe(); err != nil {
			log.Fatalf("DNS server: %v", err)
		}
	}()

	// Wait for termination signal, then shut down gracefully.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	<-quit
	log.Println("shutting down…")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = mgmtSrv.Shutdown(ctx)
	_ = dnsSrv.Shutdown()
	log.Println("bye")
}

// handleRequest processes a single DNS query through the pipeline:
//
//	blocked? → cached? → upstream → auto-learn / cache → forward response
func handleRequest(
	w dns.ResponseWriter, r *dns.Msg,
	cfg *config.Config,
	bl *blocklist.Blocklist,
	c *cache.Cache,
	ql *logger.QueryLogger,
	client *dns.Client,
) {
	if len(r.Question) == 0 {
		return
	}

	start := time.Now()
	q := r.Question[0]
	domain := strings.ToLower(strings.TrimSuffix(q.Name, "."))
	clientIP := w.RemoteAddr().String()
	qtype := dns.TypeToString[q.Qtype]

	atomic.AddInt64(&cntTotal, 1)

	// --- Local blocklist ---
	if blocked, _ := bl.IsBlocked(domain); blocked {
		m := new(dns.Msg)
		m.SetReply(r)
		m.Rcode = dns.RcodeNameError
		_ = w.WriteMsg(m)
		atomic.AddInt64(&cntLocalBlock, 1)
		ql.Write(clientIP, domain, qtype, "LOCAL_BLOCK", ms(start))
		return
	}

	// --- Cache ---
	cacheKey := fmt.Sprintf("%s:%d", domain, q.Qtype)
	if cached := c.Get(cacheKey); cached != nil {
		cached.SetReply(r)
		_ = w.WriteMsg(cached)
		atomic.AddInt64(&cntCached, 1)
		ql.Write(clientIP, domain, qtype, "CACHED", ms(start))
		return
	}

	// --- Forward to upstream ---
	resp, _, err := client.Exchange(r, cfg.UpstreamDNS)
	if err != nil {
		dns.HandleFailed(w, r)
		atomic.AddInt64(&cntError, 1)
		ql.Write(clientIP, domain, qtype, "ERROR", ms(start))
		return
	}

	// --- Auto-learn: detect upstream block responses by configured IPs ---
	// NextDNS returns "0.0.0.0"; AdGuard Family returns "94.140.14.35".
	// The list is configurable via block_detect_ips in config.yaml.
	if q.Qtype == dns.TypeA && len(cfg.BlockDetectIPs) > 0 {
		for _, ans := range resp.Answer {
			a, ok := ans.(*dns.A)
			if !ok {
				continue
			}
			aStr := a.A.String()
			for _, blockIP := range cfg.BlockDetectIPs {
				if aStr == blockIP {
					if err := bl.Append(domain); err != nil {
						log.Printf("auto-learn append: %v", err)
					}
					_ = w.WriteMsg(resp)
					atomic.AddInt64(&cntAutoLearned, 1)
					ql.Write(clientIP, domain, qtype, "AUTO_LEARNED", ms(start))
					return
				}
			}
		}
	}

	// --- Cache and forward ---
	c.Set(cacheKey, resp)
	_ = w.WriteMsg(resp)
	atomic.AddInt64(&cntForwarded, 1)
	ql.Write(clientIP, domain, qtype, "FORWARDED", ms(start))
}

func ms(start time.Time) int64 { return time.Since(start).Milliseconds() }

// buildManagementServer constructs the HTTP management API server.
//
// Endpoints:
//
//	GET  /health        – liveness check
//	GET  /metrics       – counters and sizes as JSON
//	POST /reload        – force reload blocklist from file
//	POST /cache/flush   – flush the DNS response cache
func buildManagementServer(
	addr string,
	bl *blocklist.Blocklist,
	c *cache.Cache,
	startTime *time.Time,
) *http.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
			"uptime": time.Since(*startTime).Round(time.Second).String(),
		})
	})

	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		total := atomic.LoadInt64(&cntTotal)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"uptime_sec":     int64(time.Since(*startTime).Seconds()),
			"total":          total,
			"local_block":    atomic.LoadInt64(&cntLocalBlock),
			"auto_learned":   atomic.LoadInt64(&cntAutoLearned),
			"cached":         atomic.LoadInt64(&cntCached),
			"forwarded":      atomic.LoadInt64(&cntForwarded),
			"error":          atomic.LoadInt64(&cntError),
			"cache_size":     c.Size(),
			"blocklist_size": bl.Count(),
		})
	})

	mux.HandleFunc("/reload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		if err := bl.Load(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  "reloaded",
			"entries": bl.Count(),
		})
	})

	mux.HandleFunc("/cache/flush", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		before := c.Size()
		c.Flush()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  "flushed",
			"evicted": before,
		})
	})

	return &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}
}
