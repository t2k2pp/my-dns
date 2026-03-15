// Package blocklist manages a domain blocklist loaded from a text file.
// The list supports suffix matching: "example.com" blocks "sub.example.com".
// All public methods are safe for concurrent use.
package blocklist

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Blocklist manages a set of blocked domains backed by a flat text file.
type Blocklist struct {
	mu       sync.RWMutex
	entries  []string
	filePath string
	modTime  time.Time
}

// New creates a Blocklist backed by filePath.
// Call Load to populate it before use.
func New(filePath string) *Blocklist {
	return &Blocklist{filePath: filePath}
}

// Load reads (or re-reads) the blocklist file into memory.
// A missing file is treated as an empty list (no error).
func (bl *Blocklist) Load() error {
	info, err := os.Stat(bl.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			bl.mu.Lock()
			bl.entries = nil
			bl.mu.Unlock()
			return nil
		}
		return fmt.Errorf("stat blocklist: %w", err)
	}

	f, err := os.Open(bl.filePath)
	if err != nil {
		return fmt.Errorf("open blocklist: %w", err)
	}
	defer f.Close()

	var entries []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		entries = append(entries, strings.ToLower(line))
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan blocklist: %w", err)
	}

	bl.mu.Lock()
	bl.entries = entries
	bl.modTime = info.ModTime()
	bl.mu.Unlock()
	return nil
}

// ReloadIfChanged reloads only when the file's mtime has advanced.
// Returns true when a reload actually occurred.
func (bl *Blocklist) ReloadIfChanged() (bool, error) {
	info, err := os.Stat(bl.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	bl.mu.RLock()
	changed := info.ModTime().After(bl.modTime)
	bl.mu.RUnlock()
	if !changed {
		return false, nil
	}
	return true, bl.Load()
}

// IsBlocked reports whether domain (or any parent domain) is in the blocklist.
// Returns (matched, matchedRule).
func (bl *Blocklist) IsBlocked(domain string) (bool, string) {
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))
	bl.mu.RLock()
	defer bl.mu.RUnlock()
	for _, entry := range bl.entries {
		if domain == entry || strings.HasSuffix(domain, "."+entry) {
			return true, entry
		}
	}
	return false, ""
}

// Append adds domain to the in-memory list and appends it to the backing file.
// Silently ignores duplicates.
func (bl *Blocklist) Append(domain string) error {
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))

	bl.mu.Lock()
	for _, e := range bl.entries {
		if e == domain {
			bl.mu.Unlock()
			return nil
		}
	}
	bl.entries = append(bl.entries, domain)
	bl.mu.Unlock()

	f, err := os.OpenFile(bl.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open blocklist for append: %w", err)
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, domain)
	return err
}

// Count returns the number of entries currently loaded in memory.
func (bl *Blocklist) Count() int {
	bl.mu.RLock()
	defer bl.mu.RUnlock()
	return len(bl.entries)
}

// FilePath returns the path to the backing file.
func (bl *Blocklist) FilePath() string { return bl.filePath }
