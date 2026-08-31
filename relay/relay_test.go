package relay

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestPacketBufferPoolUsesPointerOwnership(t *testing.T) {
	buf, ok := bufferPool.Get().(*packetBuffer)
	if !ok {
		t.Fatalf("bufferPool.Get() returned an unexpected type")
	}

	packet := Packet{data: buf[:1], n: 1, buffer: buf}
	if packet.buffer != buf {
		t.Fatal("packet did not retain its pooled buffer")
	}
	releasePacketBuffer(packet.buffer)
}

func TestRemoveConnectionRejectsStaleTarget(t *testing.T) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	server := &UDPProxyServer{}
	replacement := &DestinationConn{conn: conn}
	server.connections.Store("connection", replacement)
	server.connectionCount.Store(1)

	if server.removeConnection("connection", &DestinationConn{}) {
		t.Fatal("removed a connection that did not match the targeted entry")
	}
	if got := server.connectionCount.Load(); got != 1 {
		t.Fatalf("connectionCount = %d after stale removal, want 1", got)
	}
	if got, ok := server.connections.Load("connection"); !ok || got != replacement {
		t.Fatal("replacement connection was removed by stale cleanup")
	}

	if !server.removeConnection("connection", replacement) {
		t.Fatal("failed to remove the targeted connection")
	}
	if got := server.connectionCount.Load(); got != 0 {
		t.Fatalf("connectionCount = %d after removal, want 0", got)
	}
}

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
