package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

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
