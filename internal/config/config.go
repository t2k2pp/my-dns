package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds all tunable parameters for the DNS server.
// Fields can be set via YAML file; missing keys fall back to defaults.
type Config struct {
	ListenIP                string   `yaml:"listen_ip"`
	ListenPort              int      `yaml:"listen_port"`
	UpstreamDNS             string   `yaml:"upstream_dns"`
	BlocklistFile           string   `yaml:"blocklist_file"`
	LogFile                 string   `yaml:"log_file"`
	LogMaxBytes             int64    `yaml:"log_max_bytes"`
	CacheTTL                int      `yaml:"cache_ttl"`
	BlocklistReloadInterval int      `yaml:"blocklist_reload_interval"`
	UpstreamTimeoutSec      int      `yaml:"upstream_timeout_sec"`
	ManagementAddr          string   `yaml:"management_addr"`
	// BlockDetectIPs lists the IP addresses that the upstream DNS returns
	// when it blocks a domain. Matched A-record responses trigger auto-learn.
	// NextDNS returns "0.0.0.0"; AdGuard Family returns "94.140.14.35".
	BlockDetectIPs          []string `yaml:"block_detect_ips"`
}

// Defaults returns a Config populated with sensible defaults.
func Defaults() *Config {
	return &Config{
		ListenIP:                "0.0.0.0",
		ListenPort:              53,
		UpstreamDNS:             "45.90.28.0:53",
		BlocklistFile:           "blocklist.txt",
		LogFile:                 "query.log",
		LogMaxBytes:             10 * 1024 * 1024, // 10 MB
		CacheTTL:                300,              // 5 minutes
		BlocklistReloadInterval: 60,              // 1 minute
		UpstreamTimeoutSec:      5,
		ManagementAddr:          "127.0.0.1:8080",
		BlockDetectIPs:          []string{"0.0.0.0"}, // NextDNS default
	}
}

// Load reads a YAML config file and merges it over the defaults.
// If the file does not exist the defaults are returned without error.
func Load(path string) (*Config, error) {
	cfg := Defaults()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}
	return cfg, nil
}

// ListenAddr returns the "host:port" listen address.
func (c *Config) ListenAddr() string {
	return fmt.Sprintf("%s:%d", c.ListenIP, c.ListenPort)
}
