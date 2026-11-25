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
	// gen counts invalidations. A GET records it before its request and
	// stores its response only if no invalidation happened meanwhile, so a
	// list fetched while a write runs can't put stale data back. It is one
	// counter for every type: a write keeps any overlapping list GET from
	// being stored, which costs a cache miss, never correctness.
	gen uint64
	// writes counts writes in flight (BeginWrite). Nothing is stored while
	// one runs, since a list fetched then may predate it.
	writes int
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

// Generation returns the current invalidation count (see SetIfGeneration).
func (c *Cache) Generation() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.gen
}

// Set stores a response in the cache with the configured TTL.
func (c *Cache) Set(key string, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.set(key, data)
}

// SetIfGeneration stores a response only if no invalidation has happened
// since gen was read with Generation, and no write is in flight. The check and the store share the
// lock, so an invalidation can't slip in between them. It reports whether
// the response was stored.
func (c *Cache) SetIfGeneration(key string, data []byte, gen uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gen != gen || c.writes > 0 {
		return false
	}
	c.set(key, data)
	return true
}

// BeginWrite marks a write to prefix's resources as in flight and invalidates
// them. Call the returned function once the write is over, whatever its
// outcome: it invalidates again and ends the write.
func (c *Cache) BeginWrite(prefix string) func() {
	c.mu.Lock()
	c.writes++
	c.invalidate(prefix)
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.writes--
		c.invalidate(prefix)
	}
}

func (c *Cache) set(key string, data []byte) {
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
	c.invalidate(prefix)
}

func (c *Cache) invalidate(prefix string) {
	c.gen++
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
