package main

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

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
