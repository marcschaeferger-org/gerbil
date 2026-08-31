package relay

import (
	"testing"
	"time"
)

func TestEndpointCacheBoundsEntries(t *testing.T) {
	cache := newEndpointCache(2, time.Minute)
	now := time.Now()

	cache.Store("one", cachedEndpointEntry{cachedAt: now})
	cache.Store("two", cachedEndpointEntry{cachedAt: now})
	cache.Store("three", cachedEndpointEntry{cachedAt: now})

	cache.mu.Lock()
	defer cache.mu.Unlock()
	if got := len(cache.entries); got != 2 {
		t.Fatalf("cache contains %d entries, want 2", got)
	}
}

func TestEndpointCacheDeletesExpiredEntries(t *testing.T) {
	cache := newEndpointCache(2, time.Second)
	now := time.Now()
	cache.Store("expired", cachedEndpointEntry{cachedAt: now.Add(-time.Second)})
	cache.Store("fresh", cachedEndpointEntry{cachedAt: time.Now().Add(time.Hour)})

	cache.DeleteExpired(now)

	if _, ok := cache.Load("expired"); ok {
		t.Fatal("expired entry remains in cache")
	}
	if _, ok := cache.Load("fresh"); !ok {
		t.Fatal("fresh entry was removed from cache")
	}
}
