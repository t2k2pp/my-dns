// log-analyzer reads DNS query logs (current file + ZIP archives) and
// produces summary reports useful for day-to-day operation and auditing.
//
// Usage:
//
//	log-analyzer [flags] <command> [args]
//
// Commands:
//
//	summary        Overall statistics
//	top-domains    Most queried domains
//	top-blocked    Most blocked/auto-learned domains
//	top-clients    Most active client IPs
//	timeline       Query volume by hour of day
//	search <pat>   Find records whose domain contains pat (case-insensitive)
//	tail [n]       Show the last n records (default 50)
//	errors         List all ERROR records
//
// Flags:
//
//	-log string      Log file path (default "query.log")
//	-from YYYY-MM-DD Start of date range
//	-to   YYYY-MM-DD End of date range (inclusive)
//	-client string   Filter by client IP substring
//	-action string   Comma-separated actions to include
//	-qtype  string   Comma-separated query types to include
//	-n int           Top-N limit (default 20)
//	-no-archive      Do not read ZIP archives
//	-json            Output as JSON instead of table
package main

import (
	"archive/zip"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// record holds one parsed log line.
type record struct {
	Timestamp time.Time
	ClientIP  string
	Domain    string
	QueryType string
	Action    string
	LatencyMs int64
}

// ---- CLI flags ----

var (
	flagLog       = flag.String("log", "query.log", "log file path")
	flagFrom      = flag.String("from", "", "start date YYYY-MM-DD")
	flagTo        = flag.String("to", "", "end date YYYY-MM-DD")
	flagClient    = flag.String("client", "", "filter by client IP substring")
	flagAction    = flag.String("action", "", "comma-separated actions to include")
	flagQtype     = flag.String("qtype", "", "comma-separated query types to include")
	flagN         = flag.Int("n", 20, "top-N limit")
	flagNoArchive = flag.Bool("no-archive", false, "skip ZIP archives")
	flagJSON      = flag.Bool("json", false, "output as JSON")
)

func main() {
	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		usage()
		os.Exit(1)
	}
	cmd := args[0]
	cmdArgs := args[1:]

	records, err := loadRecords()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading records: %v\n", err)
		os.Exit(1)
	}
	records = applyFilters(records)

	switch cmd {
	case "summary":
		cmdSummary(records)
	case "top-domains":
		cmdTopDomains(records)
	case "top-blocked":
		cmdTopBlocked(records)
	case "top-clients":
		cmdTopClients(records)
	case "timeline":
		cmdTimeline(records)
	case "search":
		if len(cmdArgs) == 0 {
			fmt.Fprintln(os.Stderr, "search requires a pattern argument")
			os.Exit(1)
		}
		cmdSearch(records, cmdArgs[0])
	case "tail":
		n := 50
		if len(cmdArgs) > 0 {
			if v, err := strconv.Atoi(cmdArgs[0]); err == nil && v > 0 {
				n = v
			}
		}
		cmdTail(records, n)
	case "errors":
		cmdErrors(records)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: log-analyzer [flags] <command> [args]

Commands:
  summary            Overall statistics
  top-domains        Most queried domains
  top-blocked        Most blocked/auto-learned domains
  top-clients        Most active client IPs
  timeline           Query volume by hour of day
  search <pattern>   Find records whose domain contains pattern
  tail [n]           Show last n records (default 50)
  errors             Show all ERROR records

Flags:
`)
	flag.PrintDefaults()
}

// ---- Data loading ----

func loadRecords() ([]record, error) {
	var all []record

	// Current log file.
	if recs, err := readFile(*flagLog); err == nil {
		all = append(all, recs...)
	} else if !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "warn: read %s: %v\n", *flagLog, err)
	}

	// ZIP archives in the same directory.
	if !*flagNoArchive {
		dir := filepath.Dir(*flagLog)
		base := strings.TrimSuffix(filepath.Base(*flagLog), filepath.Ext(*flagLog))
		pattern := filepath.Join(dir, base+"_*.zip")
		zips, _ := filepath.Glob(pattern)
		for _, zp := range zips {
			recs, err := readZip(zp)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warn: read %s: %v\n", zp, err)
				continue
			}
			all = append(all, recs...)
		}
	}

	// Sort by timestamp ascending.
	sort.Slice(all, func(i, j int) bool {
		return all[i].Timestamp.Before(all[j].Timestamp)
	})
	return all, nil
}

func readFile(path string) ([]record, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseCSV(f)
}

func readZip(path string) ([]record, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	var all []record
	for _, zf := range zr.File {
		if !strings.HasSuffix(strings.ToLower(zf.Name), ".log") &&
			!strings.HasSuffix(strings.ToLower(zf.Name), ".csv") {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			continue
		}
		recs, err := parseCSV(rc)
		rc.Close()
		if err != nil {
			continue
		}
		all = append(all, recs...)
	}
	return all, nil
}

func parseCSV(r io.Reader) ([]record, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1 // allow variable columns (old 4-col vs new 6-col)
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}

	var records []record
	for _, row := range rows {
		if len(row) < 4 {
			continue
		}
		// Skip header row.
		if row[0] == "Timestamp" {
			continue
		}
		ts, err := time.Parse(time.RFC3339, row[0])
		if err != nil {
			continue
		}
		rec := record{
			Timestamp: ts,
			ClientIP:  row[1],
			Domain:    row[2],
		}
		if len(row) >= 6 {
			// New format: Timestamp,ClientIP,Domain,QueryType,Action,Latency_ms
			rec.QueryType = row[3]
			rec.Action = row[4]
			rec.LatencyMs, _ = strconv.ParseInt(row[5], 10, 64)
		} else {
			// Old format: Timestamp,ClientIP,Domain,Action
			rec.QueryType = "A"
			rec.Action = row[3]
		}
		records = append(records, rec)
	}
	return records, nil
}

// ---- Filtering ----

func applyFilters(records []record) []record {
	from, _ := parseDate(*flagFrom)
	to, _ := parseDate(*flagTo)
	if !to.IsZero() {
		to = to.Add(24*time.Hour - time.Second) // end of day
	}

	actionSet := splitSet(strings.ToUpper(*flagAction))
	qtypeSet := splitSet(strings.ToUpper(*flagQtype))

	var out []record
	for _, rec := range records {
		if !from.IsZero() && rec.Timestamp.Before(from) {
			continue
		}
		if !to.IsZero() && rec.Timestamp.After(to) {
			continue
		}
		if *flagClient != "" && !strings.Contains(rec.ClientIP, *flagClient) {
			continue
		}
		if len(actionSet) > 0 && !actionSet[strings.ToUpper(rec.Action)] {
			continue
		}
		if len(qtypeSet) > 0 && !qtypeSet[strings.ToUpper(rec.QueryType)] {
			continue
		}
		out = append(out, rec)
	}
	return out
}

func parseDate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.ParseInLocation("2006-01-02", s, time.UTC)
}

func splitSet(s string) map[string]bool {
	if s == "" {
		return nil
	}
	m := map[string]bool{}
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			m[p] = true
		}
	}
	return m
}

// ---- Commands ----

func cmdSummary(records []record) {
	if len(records) == 0 {
		fmt.Println("No records found.")
		return
	}

	actionCounts := map[string]int{}
	domainSet := map[string]bool{}
	clientSet := map[string]bool{}
	var totalLatency int64
	var latencyCount int64
	var first, last time.Time

	for _, rec := range records {
		actionCounts[rec.Action]++
		domainSet[rec.Domain] = true
		clientSet[rec.ClientIP] = true
		if rec.Action == "FORWARDED" || rec.Action == "AUTO_LEARNED" {
			totalLatency += rec.LatencyMs
			latencyCount++
		}
		if first.IsZero() || rec.Timestamp.Before(first) {
			first = rec.Timestamp
		}
		if rec.Timestamp.After(last) {
			last = rec.Timestamp
		}
	}

	total := len(records)
	actions := []string{"FORWARDED", "CACHED", "LOCAL_BLOCK", "AUTO_LEARNED", "ERROR"}

	if *flagJSON {
		breakdown := map[string]any{}
		for _, a := range actions {
			breakdown[strings.ToLower(a)] = actionCounts[a]
		}
		out := map[string]any{
			"period_start":      first.Format(time.RFC3339),
			"period_end":        last.Format(time.RFC3339),
			"total":             total,
			"unique_domains":    len(domainSet),
			"unique_clients":    len(clientSet),
			"action_breakdown":  breakdown,
		}
		if latencyCount > 0 {
			out["avg_upstream_latency_ms"] = totalLatency / latencyCount
		}
		printJSON(out)
		return
	}

	fmt.Println("=== DNS Query Log Summary ===")
	fmt.Printf("Period:           %s – %s\n", first.Format("2006-01-02 15:04"), last.Format("2006-01-02 15:04"))
	fmt.Printf("Total queries:    %s\n", fmtInt(total))
	fmt.Printf("Unique domains:   %s\n", fmtInt(len(domainSet)))
	fmt.Printf("Unique clients:   %s\n", fmtInt(len(clientSet)))
	fmt.Println()
	fmt.Println("Action breakdown:")
	for _, a := range actions {
		cnt := actionCounts[a]
		if cnt == 0 {
			continue
		}
		pct := float64(cnt) / float64(total) * 100
		fmt.Printf("  %-15s %8s  %5.1f%%\n", a, fmtInt(cnt), pct)
	}
	if latencyCount > 0 {
		fmt.Printf("\nAvg upstream latency: %d ms (FORWARDED + AUTO_LEARNED)\n", totalLatency/latencyCount)
	}
}

func cmdTopDomains(records []record) {
	counts := map[string]int{}
	for _, rec := range records {
		counts[rec.Domain]++
	}
	printTopN("Top Queried Domains", "Domain", counts, *flagN)
}

func cmdTopBlocked(records []record) {
	counts := map[string]int{}
	for _, rec := range records {
		if rec.Action == "LOCAL_BLOCK" || rec.Action == "AUTO_LEARNED" {
			counts[rec.Domain]++
		}
	}
	printTopN("Top Blocked Domains", "Domain", counts, *flagN)
}

func cmdTopClients(records []record) {
	counts := map[string]int{}
	for _, rec := range records {
		counts[rec.ClientIP]++
	}
	printTopN("Top Client IPs", "ClientIP", counts, *flagN)
}

func cmdTimeline(records []record) {
	// Bucket by hour of day (0-23).
	hourCounts := make([]int, 24)
	for _, rec := range records {
		hourCounts[rec.Timestamp.UTC().Hour()]++
	}

	maxVal := 0
	for _, v := range hourCounts {
		if v > maxVal {
			maxVal = v
		}
	}

	if *flagJSON {
		hours := make([]map[string]any, 24)
		for h, cnt := range hourCounts {
			hours[h] = map[string]any{"hour": h, "count": cnt}
		}
		printJSON(hours)
		return
	}

	fmt.Printf("=== Query Volume by Hour (UTC) — total %s queries ===\n", fmtInt(len(records)))
	barWidth := 40
	for h, cnt := range hourCounts {
		bar := ""
		if maxVal > 0 {
			filled := int(math.Round(float64(cnt) / float64(maxVal) * float64(barWidth)))
			bar = strings.Repeat("█", filled)
		}
		fmt.Printf("%02d:00  %-40s  %s\n", h, bar, fmtInt(cnt))
	}
}

func cmdSearch(records []record, pattern string) {
	pattern = strings.ToLower(pattern)
	var matched []record
	for _, rec := range records {
		if strings.Contains(rec.Domain, pattern) {
			matched = append(matched, rec)
		}
	}
	if len(matched) == 0 {
		fmt.Printf("No records matching %q\n", pattern)
		return
	}
	printRecords(fmt.Sprintf("Search results for %q (%d records)", pattern, len(matched)), matched, 0)
}

func cmdTail(records []record, n int) {
	start := len(records) - n
	if start < 0 {
		start = 0
	}
	printRecords(fmt.Sprintf("Last %d records", n), records[start:], 0)
}

func cmdErrors(records []record) {
	var errs []record
	for _, rec := range records {
		if rec.Action == "ERROR" {
			errs = append(errs, rec)
		}
	}
	if len(errs) == 0 {
		fmt.Println("No ERROR records found.")
		return
	}
	printRecords(fmt.Sprintf("ERROR records (%d)", len(errs)), errs, 0)
}

// ---- Output helpers ----

type kv struct {
	Key   string
	Value int
}

func printTopN(title, colName string, counts map[string]int, n int) {
	ranked := make([]kv, 0, len(counts))
	for k, v := range counts {
		ranked = append(ranked, kv{k, v})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Value != ranked[j].Value {
			return ranked[i].Value > ranked[j].Value
		}
		return ranked[i].Key < ranked[j].Key
	})
	if n > 0 && len(ranked) > n {
		ranked = ranked[:n]
	}

	if *flagJSON {
		out := make([]map[string]any, len(ranked))
		for i, kv := range ranked {
			out[i] = map[string]any{"rank": i + 1, "count": kv.Value, "name": kv.Key}
		}
		printJSON(out)
		return
	}

	fmt.Printf("=== Top %d %s ===\n", len(ranked), title)
	fmt.Printf("%-6s  %-8s  %s\n", "Rank", "Count", colName)
	fmt.Println(strings.Repeat("-", 60))
	for i, kv := range ranked {
		fmt.Printf("%-6d  %-8s  %s\n", i+1, fmtInt(kv.Value), kv.Key)
	}
}

func printRecords(title string, records []record, _ int) {
	if *flagJSON {
		out := make([]map[string]any, len(records))
		for i, rec := range records {
			out[i] = map[string]any{
				"timestamp":  rec.Timestamp.Format(time.RFC3339),
				"client_ip": rec.ClientIP,
				"domain":    rec.Domain,
				"qtype":     rec.QueryType,
				"action":    rec.Action,
				"latency_ms": rec.LatencyMs,
			}
		}
		printJSON(out)
		return
	}

	fmt.Printf("=== %s ===\n", title)
	fmt.Printf("%-20s  %-18s  %-6s  %-12s  %s\n", "Timestamp", "ClientIP", "QType", "Action", "Domain")
	fmt.Println(strings.Repeat("-", 90))
	for _, rec := range records {
		fmt.Printf("%-20s  %-18s  %-6s  %-12s  %s\n",
			rec.Timestamp.UTC().Format("2006-01-02 15:04:05"),
			rec.ClientIP,
			rec.QueryType,
			rec.Action,
			rec.Domain,
		)
	}
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func fmtInt(n int) string {
	s := strconv.Itoa(n)
	out := []byte{}
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, byte(c))
	}
	return string(out)
}
