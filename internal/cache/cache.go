// Package cache provides a simple TTL-based in-memory DNS response cache.
// All methods are safe for concurrent use.
package cache

import (
	"sync"
	"time"

	"github.com/miekg/dns"
)

type entry struct {
	msg       *dns.Msg
	expiresAt time.Time
}

// Cache is a concurrent, TTL-based DNS response cache.
type Cache struct {
	mu      sync.RWMutex
	entries map[string]entry
	ttl     time.Duration
}

// New creates a Cache with the given default TTL and starts a background
// goroutine that evicts expired entries every minute.
func New(ttl time.Duration) *Cache {
	c := &Cache{
		entries: make(map[string]entry),
		ttl:     ttl,
	}
	go c.evictLoop()
	return c
}

// Get returns a deep copy of the cached message for key, or nil if
// the entry is absent or has expired.
func (c *Cache) Get(key string) *dns.Msg {
	c.mu.RLock()
	e, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok || time.Now().After(e.expiresAt) {
		return nil
	}
	return e.msg.Copy()
}

// Set stores a deep copy of msg under key using the default TTL.
func (c *Cache) Set(key string, msg *dns.Msg) {
	c.mu.Lock()
	c.entries[key] = entry{msg: msg.Copy(), expiresAt: time.Now().Add(c.ttl)}
	c.mu.Unlock()
}

// Size returns the total number of entries in the cache (including expired,
// not-yet-evicted ones).
func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// Flush removes all cached entries immediately.
func (c *Cache) Flush() {
	c.mu.Lock()
	c.entries = make(map[string]entry)
	c.mu.Unlock()
}

// evictLoop runs every 60 seconds and removes expired entries.
func (c *Cache) evictLoop() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		c.mu.Lock()
		for k, e := range c.entries {
			if now.After(e.expiresAt) {
				delete(c.entries, k)
			}
		}
		c.mu.Unlock()
	}
}
