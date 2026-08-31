package relay

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestValidateRemoteConfigURL(t *testing.T) {
	tests := []struct {
		name      string
		serverURL string
		wantErr   bool
	}{
		{name: "HTTPS", serverURL: "https://control.example.com"},
		{name: "HTTP rejected", serverURL: "http://control.example.com", wantErr: true},
		{name: "missing scheme", serverURL: "control.example.com", wantErr: true},
		{name: "unsupported scheme", serverURL: "ftp://control.example.com", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRemoteConfigURL(tt.serverURL)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateRemoteConfigURL() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestControlPlaneClientRejectsInsecureRedirect(t *testing.T) {
	client := NewControlPlaneHTTPClient()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://control.example.com", nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := client.CheckRedirect(req, nil); err == nil {
		t.Fatal("expected HTTP redirect to be rejected")
	}
}

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
