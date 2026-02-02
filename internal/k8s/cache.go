package k8s

import (
	"sync"
	"time"
)

// ServiceInfo contains resolved Kubernetes service information
type ServiceInfo struct {
	Service   string
	Namespace string
	Pod       string
}

// CacheEntry represents a cached service info with expiration
type CacheEntry struct {
	Info      *ServiceInfo
	ExpiresAt time.Time
}

// Cache provides thread-safe caching of IP→Service mappings
type Cache struct {
	mu      sync.RWMutex
	entries map[string]*CacheEntry
	ttl     time.Duration
	maxSize int
}

// NewCache creates a new cache with given TTL
func NewCache(ttl time.Duration) *Cache {
	return &Cache{
		entries: make(map[string]*CacheEntry),
		ttl:     ttl,
		maxSize: 10000, // Max 10k entries
	}
}

// Get retrieves a value from the cache
func (c *Cache) Get(ip string) *ServiceInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.entries[ip]
	if !exists {
		return nil
	}

	// Check if expired
	if time.Now().After(entry.ExpiresAt) {
		return nil
	}

	return entry.Info
}

// Set stores a value in the cache
func (c *Cache) Set(ip string, info *ServiceInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// If cache is full, evict oldest entries (simple LRU)
	if len(c.entries) >= c.maxSize {
		c.evictOldest()
	}

	c.entries[ip] = &CacheEntry{
		Info:      info,
		ExpiresAt: time.Now().Add(c.ttl),
	}
}

// evictOldest removes expired or oldest entries
func (c *Cache) evictOldest() {
	now := time.Now()
	var toDelete []string

	// First pass: remove expired entries
	for ip, entry := range c.entries {
		if now.After(entry.ExpiresAt) {
			toDelete = append(toDelete, ip)
		}
	}

	// Delete expired entries
	for _, ip := range toDelete {
		delete(c.entries, ip)
	}

	// If still over capacity, remove 10% of entries
	if len(c.entries) >= c.maxSize {
		count := 0
		target := c.maxSize / 10 // Remove 10%
		for ip := range c.entries {
			delete(c.entries, ip)
			count++
			if count >= target {
				break
			}
		}
	}
}

// Clear removes all entries from the cache
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*CacheEntry)
}

// Size returns the current number of entries in the cache
func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}
