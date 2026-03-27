package main

import (
	"slices"
	"strings"
	"sync"
	"time"
)

// Cache is an in-memory TTL cache for metadata list endpoints.
// It caches responses for tags, correspondents, document types, storage paths,
// and custom fields — these change infrequently but are queried often by agents.
type Cache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry
	ttl     time.Duration
}

type cacheEntry struct {
	data      []byte
	expiresAt time.Time
}

// NewCache creates a new cache with the given TTL.
func NewCache(ttl time.Duration) *Cache {
	return &Cache{
		entries: make(map[string]*cacheEntry),
		ttl:     ttl,
	}
}

// Get retrieves a cached response for the given key.
// Returns the data and true if found and not expired, or nil and false otherwise.
func (c *Cache) Get(key string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.data, true
}

// Set stores a response in the cache with the configured TTL.
func (c *Cache) Set(key string, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[key] = &cacheEntry{
		data:      data,
		expiresAt: time.Now().Add(c.ttl),
	}
}

// Invalidate removes all cached entries whose keys start with the given prefix.
// Used to invalidate a resource type's cache on create/update/delete.
func (c *Cache) Invalidate(prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for key := range c.entries {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			delete(c.entries, key)
		}
	}
}

// cacheable paths that should be cached (list endpoints for metadata).
var cacheablePrefixes = []string{
	"/api/tags/",
	"/api/correspondents/",
	"/api/document_types/",
	"/api/storage_paths/",
	"/api/custom_fields/",
}

// isCacheable returns true if the given path should be cached.
func isCacheable(path string) bool {
	return slices.Contains(cacheablePrefixes, path)
}

// cachePrefix returns the invalidation prefix for a path.
// E.g., "/api/tags/5/" returns "/api/tags/".
func cachePrefix(path string) string {
	for _, prefix := range cacheablePrefixes {
		if strings.HasPrefix(path, prefix) {
			return prefix
		}
	}
	return ""
}
