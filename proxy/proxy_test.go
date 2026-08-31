package proxy

import (
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/fosrl/gerbil/logger"
)

func TestLoadSNITunnelIdleTimeout(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected time.Duration
	}{
		{name: "configured", value: "30s", expected: 30 * time.Second},
		{name: "invalid", value: "invalid", expected: defaultSNITunnelIdleTimeout},
		{name: "non-positive", value: "0s", expected: defaultSNITunnelIdleTimeout},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GERBIL_SNI_TUNNEL_IDLE_TIMEOUT", tt.value)
			if got := loadSNITunnelIdleTimeout(); got != tt.expected {
				t.Fatalf("loadSNITunnelIdleTimeout() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestPipeClosesIdleTunnel(t *testing.T) {
	proxy := newPipeTestProxy(200 * time.Millisecond)
	clientConn, clientPeer := net.Pipe()
	targetConn, targetPeer := net.Pipe()
	defer clientPeer.Close()
	defer targetPeer.Close()

	done := make(chan struct{})
	go func() {
		proxy.pipe("example.com", clientConn, targetConn, clientConn)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("idle tunnel did not close after its deadline")
	}
}

func TestPipeRefreshesIdleDeadlineAfterTraffic(t *testing.T) {
	proxy := newPipeTestProxy(200 * time.Millisecond)
	clientConn, clientPeer := net.Pipe()
	targetConn, targetPeer := net.Pipe()
	defer clientPeer.Close()
	defer targetPeer.Close()

	done := make(chan struct{})
	go func() {
		proxy.pipe("example.com", clientConn, targetConn, clientConn)
		close(done)
	}()

	time.Sleep(120 * time.Millisecond)
	go func() { _, _ = clientPeer.Write([]byte("x")) }()
	if _, err := io.ReadFull(targetPeer, make([]byte, 1)); err != nil {
		t.Fatalf("failed to forward tunnel traffic: %v", err)
	}

	select {
	case <-done:
		t.Fatal("tunnel closed using the original deadline after traffic")
	case <-time.After(120 * time.Millisecond):
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("tunnel did not close after the refreshed deadline")
	}
}

func newPipeTestProxy(idleTimeout time.Duration) *SNIProxy {
	logger.Init()
	return &SNIProxy{
		tunnelIdleTimeout: idleTimeout,
		bufferPool: &sync.Pool{
			New: func() interface{} {
				buf := make([]byte, 32*1024)
				return &buf
			},
		},
	}
}

func TestNewSNIProxyRequiresHTTPS(t *testing.T) {
	if _, err := NewSNIProxy(8443, "http://pangolin.example.com", "", "127.0.0.1", 443, nil, false, nil); err == nil {
		t.Fatal("expected plaintext remote config URL to be rejected")
	}
}

func TestNewSNIProxyAcceptsHTTPSRemoteConfigURL(t *testing.T) {
	if _, err := NewSNIProxy(8443, "https://pangolin.example.com", "", "127.0.0.1", 443, nil, false, nil); err != nil {
		t.Fatalf("expected valid https remote config URL to be accepted: %v", err)
	}
}

func TestNewSNIProxyAcceptsEmptyRemoteConfigURL(t *testing.T) {
	if _, err := NewSNIProxy(8443, "", "", "127.0.0.1", 443, nil, false, nil); err != nil {
		t.Fatalf("expected empty remote config URL to be accepted: %v", err)
	}
}

func TestNewSNIProxyValidatesAllowedNetworks(t *testing.T) {
	if _, err := NewSNIProxy(8443, "https://pangolin.example.com", "", "127.0.0.1", 443, nil, false, nil, "not-a-cidr"); err == nil {
		t.Fatal("expected invalid allowed network to be rejected")
	}
}

func TestRouteAPIRedirectMustRemainOnHTTPSOrigin(t *testing.T) {
	proxy, err := NewSNIProxy(8443, "https://pangolin.example.com", "", "127.0.0.1", 443, nil, false, nil)
	if err != nil {
		t.Fatalf("failed to create SNI proxy: %v", err)
	}

	original, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://pangolin.example.com/api", nil)
	if err != nil {
		t.Fatalf("failed to create original request: %v", err)
	}
	for _, redirectURL := range []string{
		"http://pangolin.example.com/api",
		"https://attacker.example.com/api",
	} {
		redirect, err := http.NewRequestWithContext(t.Context(), http.MethodPost, redirectURL, nil)
		if err != nil {
			t.Fatalf("failed to create redirect request: %v", err)
		}
		if err := proxy.httpClient.CheckRedirect(redirect, []*http.Request{original}); err == nil {
			t.Errorf("expected redirect to %s to be rejected", redirectURL)
		}
	}
}

func TestDialRouteEnforcesAllowedNetworks(t *testing.T) {
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer func() { _ = listener.Close() }()

	proxy, err := NewSNIProxy(8443, "https://pangolin.example.com", "", "127.0.0.1", 443, nil, false, nil, "127.0.0.0/8")
	if err != nil {
		t.Fatalf("failed to create SNI proxy: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
	}()

	conn, err := proxy.dialRoute(&RouteRecord{TargetHost: "127.0.0.1", TargetPort: port, remote: true})
	if err != nil {
		t.Fatalf("expected allowed route to connect: %v", err)
	}
	_ = conn.Close()
	_ = (<-accepted).Close()

	proxy.allowedNetworks = nil
	if _, err := proxy.dialRoute(&RouteRecord{TargetHost: "127.0.0.1", TargetPort: port, remote: true}); err == nil {
		t.Fatal("expected route outside allowed networks to be rejected")
	}
}

func TestBuildProxyProtocolHeader(t *testing.T) {
	tests := []struct {
		name       string
		clientAddr string
		targetAddr string
		expected   string
	}{
		{
			name:       "IPv4 client and target",
			clientAddr: "192.168.1.100:12345",
			targetAddr: "10.0.0.1:443",
			expected:   "PROXY TCP4 192.168.1.100 10.0.0.1 12345 443\r\n",
		},
		{
			name:       "IPv6 client and target",
			clientAddr: "[2001:db8::1]:12345",
			targetAddr: "[2001:db8::2]:443",
			expected:   "PROXY TCP6 2001:db8::1 2001:db8::2 12345 443\r\n",
		},
		{
			name:       "IPv4 client with IPv6 loopback target",
			clientAddr: "192.168.1.100:12345",
			targetAddr: "[::1]:443",
			expected:   "PROXY TCP4 192.168.1.100 127.0.0.1 12345 443\r\n",
		},
		{
			name:       "IPv4 client with IPv6 target",
			clientAddr: "192.168.1.100:12345",
			targetAddr: "[2001:db8::2]:443",
			expected:   "PROXY TCP4 192.168.1.100 127.0.0.1 12345 443\r\n",
		},
		{
			name:       "IPv6 client with IPv4 target",
			clientAddr: "[2001:db8::1]:12345",
			targetAddr: "10.0.0.1:443",
			expected:   "PROXY TCP6 2001:db8::1 ::ffff:10.0.0.1 12345 443\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientTCP, err := net.ResolveTCPAddr("tcp", tt.clientAddr)
			if err != nil {
				t.Fatalf("Failed to resolve client address: %v", err)
			}

			targetTCP, err := net.ResolveTCPAddr("tcp", tt.targetAddr)
			if err != nil {
				t.Fatalf("Failed to resolve target address: %v", err)
			}

			result := buildProxyProtocolHeader(clientTCP, targetTCP)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestBuildProxyProtocolHeaderUnknownType(t *testing.T) {
	// Test with non-TCP address type
	clientAddr := &net.UDPAddr{IP: net.ParseIP("192.168.1.100"), Port: 12345}
	targetAddr := &net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 443}

	result := buildProxyProtocolHeader(clientAddr, targetAddr)
	expected := "PROXY UNKNOWN\r\n"

	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestBuildProxyProtocolHeaderFromInfo(t *testing.T) {
	proxy, err := NewSNIProxy(8443, "", "", "127.0.0.1", 443, nil, true, nil)
	if err != nil {
		t.Fatalf("Failed to create SNI proxy: %v", err)
	}

	// Test IPv4 case
	proxyInfo := &ProxyProtocolInfo{
		Protocol: "TCP4",
		SrcIP:    "10.0.0.1",
		DestIP:   "192.168.1.100",
		SrcPort:  12345,
		DestPort: 443,
	}

	targetAddr, _ := net.ResolveTCPAddr("tcp", "127.0.0.1:8080")
	header := proxy.buildProxyProtocolHeaderFromInfo(proxyInfo, targetAddr)

	expected := "PROXY TCP4 10.0.0.1 127.0.0.1 12345 8080\r\n"
	if header != expected {
		t.Errorf("Expected header '%s', got '%s'", expected, header)
	}

	// Test IPv6 case
	proxyInfo = &ProxyProtocolInfo{
		Protocol: "TCP6",
		SrcIP:    "2001:db8::1",
		DestIP:   "2001:db8::2",
		SrcPort:  12345,
		DestPort: 443,
	}

	targetAddr, _ = net.ResolveTCPAddr("tcp6", "[::1]:8080")
	header = proxy.buildProxyProtocolHeaderFromInfo(proxyInfo, targetAddr)

	expected = "PROXY TCP6 2001:db8::1 ::1 12345 8080\r\n"
	if header != expected {
		t.Errorf("Expected header '%s', got '%s'", expected, header)
	}
}
