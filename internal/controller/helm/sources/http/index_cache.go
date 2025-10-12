// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package http

import (
	"sync"
	"time"

	"github.com/go-logr/logr"
	"helm.sh/helm/v3/pkg/repo"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// CachedIndex represents a single cached repository index with metadata
type CachedIndex struct {
	Index      *repo.IndexFile
	CachedAt   time.Time
	AccessedAt time.Time // For LRU tracking
	RepoURL    string
}

// IndexCache provides thread-safe in-memory caching of Helm repository indexes
// with LRU eviction and TTL-based expiration.
//
// When maxSize is 0, the cache is disabled and all operations are no-ops.
type IndexCache struct {
	mu      sync.RWMutex
	items   map[string]*CachedIndex
	maxSize int
	ttl     time.Duration
	stopCh  chan struct{}
	log     logr.Logger
}

// NewIndexCache creates a new IndexCache with the specified configuration.
//
// Parameters:
//   - maxSize: Maximum number of indexes to cache. If 0, caching is disabled.
//   - ttl: Time-to-live for cached indexes. After this duration, entries are expired.
//
// The cache starts a background goroutine that periodically cleans up expired entries.
// Call Close() to stop this goroutine when the cache is no longer needed.
func NewIndexCache(maxSize int, ttl time.Duration) *IndexCache {
	c := &IndexCache{
		items:   make(map[string]*CachedIndex),
		maxSize: maxSize,
		ttl:     ttl,
		stopCh:  make(chan struct{}),
		log:     logf.Log.WithName("index-cache"),
	}

	if maxSize > 0 {
		c.log.Info("Index cache initialized", "maxSize", maxSize, "ttl", ttl)
		go c.startCleanup()
	} else {
		c.log.Info("Index cache disabled (maxSize=0)")
	}

	return c
}

// Get retrieves a cached index by repository name.
// Returns the index and true if found and not expired, nil and false otherwise.
//
// This method updates the AccessedAt timestamp for LRU tracking.
// If the cache is disabled (maxSize=0), always returns nil, false.
func (c *IndexCache) Get(repoName string) (*repo.IndexFile, bool) {
	if c.maxSize == 0 {
		return nil, false // Cache disabled
	}

	c.mu.RLock()
	cached, exists := c.items[repoName]
	c.mu.RUnlock()

	if !exists {
		c.log.V(1).Info("Cache miss - not found", "repo", repoName)
		return nil, false
	}

	// Check if expired
	if c.isExpired(cached) {
		c.log.V(1).Info("Cache miss - expired", "repo", repoName, "age", time.Since(cached.CachedAt))
		return nil, false
	}

	// Update access time for LRU (requires write lock)
	c.mu.Lock()
	cached.AccessedAt = time.Now()
	c.mu.Unlock()

	c.log.V(1).Info("Cache hit", "repo", repoName)
	return cached.Index, true
}

// Set stores an index in the cache with the current timestamp.
// If the cache is full, evicts the least recently accessed item.
//
// If the cache is disabled (maxSize=0), this is a no-op.
func (c *IndexCache) Set(repoName string, repoURL string, index *repo.IndexFile) {
	if c.maxSize == 0 {
		return // Cache disabled
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict oldest if at capacity
	if c.maxSize > 0 && len(c.items) >= c.maxSize {
		c.evictOldest()
	}

	now := time.Now()
	c.items[repoName] = &CachedIndex{
		Index:      index,
		CachedAt:   now,
		AccessedAt: now,
		RepoURL:    repoURL,
	}

	c.log.V(1).Info("Cached index", "repo", repoName, "url", repoURL)
}

// Clear removes all cached indexes.
// If the cache is disabled (maxSize=0), this is a no-op.
func (c *IndexCache) Clear() {
	if c.maxSize == 0 {
		return // Cache disabled
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	count := len(c.items)
	c.items = make(map[string]*CachedIndex)

	c.log.Info("Cache cleared", "itemsRemoved", count)
}

// Close stops the background cleanup goroutine.
// Should be called when the cache is no longer needed.
func (c *IndexCache) Close() {
	if c.maxSize == 0 {
		return // Cache disabled, no goroutine running
	}

	close(c.stopCh)
}

// isExpired checks if a cached index has exceeded its TTL.
// Must be called with at least a read lock held.
func (c *IndexCache) isExpired(cached *CachedIndex) bool {
	return time.Since(cached.CachedAt) > c.ttl
}

// evictOldest removes the least recently accessed item from the cache.
// Must be called with write lock held.
func (c *IndexCache) evictOldest() {
	if len(c.items) == 0 {
		return
	}

	var oldestName string
	var oldestTime time.Time

	// Find item with oldest AccessedAt
	for name, cached := range c.items {
		if oldestName == "" || cached.AccessedAt.Before(oldestTime) {
			oldestName = name
			oldestTime = cached.AccessedAt
		}
	}

	delete(c.items, oldestName)
	c.log.Info("Evicted index from cache", "repo", oldestName, "reason", "size limit")
}

// startCleanup runs a background goroutine that periodically removes expired entries.
// This prevents the cache from growing unbounded when items are never accessed again.
func (c *IndexCache) startCleanup() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.cleanupExpired()
		case <-c.stopCh:
			c.log.Info("Cleanup goroutine stopping")
			return
		}
	}
}

// cleanupExpired removes all expired entries from the cache.
// Called periodically by the cleanup goroutine.
func (c *IndexCache) cleanupExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	expiredCount := 0
	for name, cached := range c.items {
		if c.isExpired(cached) {
			delete(c.items, name)
			expiredCount++
			c.log.V(1).Info("Expired index removed", "repo", name, "age", time.Since(cached.CachedAt))
		}
	}

	if expiredCount > 0 {
		c.log.Info("Cleanup completed", "expiredItems", expiredCount, "remainingItems", len(c.items))
	}
}
