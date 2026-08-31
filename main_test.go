package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fosrl/gerbil/relay"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func TestRequireControlAuth(t *testing.T) {
	originalToken := controlAPIToken
	t.Cleanup(func() { controlAPIToken = originalToken })
	controlAPIToken = "0123456789abcdef0123456789abcdef"

	called := false
	handler := requireControlAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))

	for name, authorization := range map[string]string{
		"missing": "",
		"wrong":   "Bearer wrong-token",
		"scheme":  "Basic " + controlAPIToken,
	} {
		t.Run(name, func(t *testing.T) {
			called = false
			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/peer", nil)
			if authorization != "" {
				req.Header.Set("Authorization", authorization)
			}
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, req)

			if response.Code != http.StatusUnauthorized {
				t.Fatalf("got status %d, want %d", response.Code, http.StatusUnauthorized)
			}
			if called {
				t.Fatal("protected handler was called")
			}
		})
	}

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/peer", nil)
	req.Header.Set("Authorization", "Bearer "+controlAPIToken)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, req)

	if response.Code != http.StatusNoContent {
		t.Fatalf("got status %d, want %d", response.Code, http.StatusNoContent)
	}
	if !called {
		t.Fatal("authenticated request did not reach protected handler")
	}
}

func TestLoadRemoteConfigRejectsRedirect(t *testing.T) {
	var redirected atomic.Bool
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirected.Store(true)
	}))
	defer redirectTarget.Close()

	redirectSource := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL, http.StatusTemporaryRedirect)
	}))
	defer redirectSource.Close()

	client := redirectSource.Client()
	client.CheckRedirect = relay.NewControlPlaneHTTPClient().CheckRedirect

	key, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	signingKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loadRemoteConfig(context.Background(), client, redirectSource.URL, key, "", signingKey); err == nil {
		t.Fatal("expected redirected remote config request to fail")
	}
	if redirected.Load() {
		t.Fatal("remote config client followed the redirect")
	}
}

func TestLoadRemoteConfigRejectsHTTP(t *testing.T) {
	var contacted atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		contacted.Store(true)
	}))
	defer server.Close()

	key, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	signingKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loadRemoteConfig(context.Background(), server.Client(), server.URL, key, "", signingKey); err == nil {
		t.Fatal("expected plaintext HTTP remote config to be rejected")
	}
	if contacted.Load() {
		t.Fatal("insecure remote config server was contacted")
	}
}

func TestLoadRemoteConfigTimesOut(t *testing.T) {
	stalled := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(250 * time.Millisecond)
	}))
	defer stalled.Close()

	client := stalled.Client()
	client.Timeout = 25 * time.Millisecond

	key, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	signingKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if _, err := loadRemoteConfig(context.Background(), client, stalled.URL, key, "", signingKey); err == nil {
		t.Fatal("expected stalled remote config request to time out")
	}
	if elapsed := time.Since(started); elapsed >= 200*time.Millisecond {
		t.Fatalf("remote config request exceeded its timeout: %v", elapsed)
	}
}

func TestLoadRemoteConfigVerifiesSignature(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"listenPort":51820,"peers":[]}`)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(remoteConfigSignatureHeader, base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, body)))
		_, _ = w.Write(body)
	}))
	defer server.Close()

	key, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	config, err := loadRemoteConfig(context.Background(), server.Client(), server.URL, key, "", publicKey)
	if err != nil {
		t.Fatalf("loadRemoteConfig() error = %v", err)
	}
	if config.ListenPort != 51820 {
		t.Fatalf("ListenPort = %d, want 51820", config.ListenPort)
	}

	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(remoteConfigSignatureHeader, base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, body)))
		_, _ = w.Write(append(body, ' '))
	})
	if _, err := loadRemoteConfig(context.Background(), server.Client(), server.URL, key, "", publicKey); err == nil {
		t.Fatal("expected modified remote config to fail signature verification")
	}
}

func TestParseRemoteConfigSigningKey(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(publicKey)
	parsed, err := parseRemoteConfigSigningKey(encoded)
	if err != nil {
		t.Fatalf("parseRemoteConfigSigningKey() error = %v", err)
	}
	if !parsed.Equal(publicKey) {
		t.Fatal("parsed signing key does not match input")
	}
	for _, invalid := range []string{"", "not-base64", base64.StdEncoding.EncodeToString([]byte("short"))} {
		if _, err := parseRemoteConfigSigningKey(invalid); err == nil {
			t.Fatalf("parseRemoteConfigSigningKey(%q) succeeded", invalid)
		}
	}
}

func TestValidateRouterDestination(t *testing.T) {
	tests := []struct {
		name  string
		dest  string
		valid bool
	}{
		{name: "http default", dest: "100.96.128.1:8080", valid: true},
		{name: "https", dest: "https://model.example:443", valid: true},
		{name: "unsupported scheme", dest: "file://100.96.128.1:80"},
		{name: "missing port", dest: "100.96.128.1"},
		{name: "named port", dest: "100.96.128.1:http"},
		{name: "invalid port", dest: "100.96.128.1:65536"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateRouterDestination(test.dest)
			if (err == nil) != test.valid {
				t.Fatalf("validateRouterDestination(%q) error = %v, want valid = %v", test.dest, err, test.valid)
			}
		})
	}
}

func TestRouterIPAllowed(t *testing.T) {
	_, allowedNetwork, err := net.ParseCIDR("100.96.0.0/16")
	if err != nil {
		t.Fatal(err)
	}
	_, defaultIPv4, err := net.ParseCIDR("0.0.0.0/0")
	if err != nil {
		t.Fatal(err)
	}
	_, defaultIPv6, err := net.ParseCIDR("::/0")
	if err != nil {
		t.Fatal(err)
	}
	_, lowerIPv4, err := net.ParseCIDR("0.0.0.0/1")
	if err != nil {
		t.Fatal(err)
	}
	_, upperIPv4, err := net.ParseCIDR("128.0.0.0/1")
	if err != nil {
		t.Fatal(err)
	}
	_, lowerIPv6, err := net.ParseCIDR("::/1")
	if err != nil {
		t.Fatal(err)
	}
	_, upperIPv6, err := net.ParseCIDR("8000::/1")
	if err != nil {
		t.Fatal(err)
	}
	networks := []net.IPNet{
		*allowedNetwork,
		*defaultIPv4,
		*defaultIPv6,
		*lowerIPv4,
		*upperIPv4,
		*lowerIPv6,
		*upperIPv6,
	}
	for _, cidr := range []string{
		"0.0.0.0/2", "64.0.0.0/2", "128.0.0.0/2", "192.0.0.0/2",
		"::/3", "2000::/3", "4000::/3", "6000::/3",
		"8000::/3", "a000::/3", "c000::/3", "e000::/3",
	} {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			t.Fatal(err)
		}
		networks = append(networks, *network)
	}

	tests := []struct {
		name    string
		ip      string
		allowed bool
	}{
		{name: "configured peer", ip: "100.96.128.1", allowed: true},
		{name: "lower split-default IPv4", ip: "100.97.128.1"},
		{name: "upper split-default IPv4", ip: "192.0.2.1"},
		{name: "lower split-default IPv6", ip: "2001:db8::1"},
		{name: "upper split-default IPv6", ip: "9000::1"},
		{name: "loopback", ip: "127.0.0.1"},
		{name: "link local metadata", ip: "169.254.169.254"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := routerIPAllowed(net.ParseIP(test.ip), networks); got != test.allowed {
				t.Fatalf("routerIPAllowed(%q) = %v, want %v", test.ip, got, test.allowed)
			}
		})
	}
}

func TestLoadOrGeneratePrivateKeyCreatesOwnerOnlyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private-key")

	key, err := loadOrGeneratePrivateKey(path)
	if err != nil {
		t.Fatalf("loadOrGeneratePrivateKey() error = %v", err)
	}

	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat private key: %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0600 {
		t.Fatalf("private key permissions = %04o, want 0600", got)
	}

	keyData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read private key: %v", err)
	}
	if string(keyData) != key.String() {
		t.Fatal("saved private key does not match generated key")
	}
}

func TestLoadOrGeneratePrivateKeyLoadsSecureExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private-key")
	want, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("generate private key: %v", err)
	}
	if err := os.WriteFile(path, []byte(want.String()), 0600); err != nil {
		t.Fatalf("write private key: %v", err)
	}

	got, err := loadOrGeneratePrivateKey(path)
	if err != nil {
		t.Fatalf("loadOrGeneratePrivateKey() error = %v", err)
	}
	if got != want {
		t.Fatal("loaded private key does not match existing key")
	}
}

func TestLoadOrGeneratePrivateKeyRejectsInsecureExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private-key")
	key, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("generate private key: %v", err)
	}
	if err := os.WriteFile(path, []byte(key.String()), 0644); err != nil {
		t.Fatalf("write private key: %v", err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatalf("set insecure private key permissions: %v", err)
	}

	_, err = loadOrGeneratePrivateKey(path)
	if err == nil || !strings.Contains(err.Error(), "insecure permissions") {
		t.Fatalf("loadOrGeneratePrivateKey() error = %v, want insecure permissions error", err)
	}
}
