package main

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

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

	originalClient := remoteConfigHTTPClient
	remoteConfigHTTPClient = redirectSource.Client()
	remoteConfigHTTPClient.CheckRedirect = originalClient.CheckRedirect
	defer func() { remoteConfigHTTPClient = originalClient }()

	key, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	if _, err := loadRemoteConfig(redirectSource.URL, key, ""); err == nil {
		t.Fatal("expected redirected remote config request to fail")
	}
	if redirected.Load() {
		t.Fatal("remote config client followed the redirect")
	}
}

func TestLoadRemoteConfigTimesOut(t *testing.T) {
	if remoteConfigHTTPClient.Timeout <= 0 {
		t.Fatal("remote config client must have a finite timeout")
	}

	stalled := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(250 * time.Millisecond)
	}))
	defer stalled.Close()

	originalClient := remoteConfigHTTPClient
	remoteConfigHTTPClient = stalled.Client()
	remoteConfigHTTPClient.Timeout = 25 * time.Millisecond
	remoteConfigHTTPClient.CheckRedirect = originalClient.CheckRedirect
	defer func() { remoteConfigHTTPClient = originalClient }()

	key, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	started := time.Now()
	if _, err := loadRemoteConfig(stalled.URL, key, ""); err == nil {
		t.Fatal("expected stalled remote config request to time out")
	}
	if elapsed := time.Since(started); elapsed >= 200*time.Millisecond {
		t.Fatalf("remote config request exceeded its timeout: %v", elapsed)
	}
}
