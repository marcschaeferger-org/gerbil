package relay

import (
	"context"
	"net/http"
	"testing"
)

func TestValidateRemoteConfigURL(t *testing.T) {
	tests := []struct {
		name          string
		serverURL     string
		allowInsecure bool
		wantErr       bool
	}{
		{name: "HTTPS", serverURL: "https://control.example.com"},
		{name: "HTTP rejected", serverURL: "http://control.example.com", wantErr: true},
		{name: "HTTP development override", serverURL: "http://localhost:3001", allowInsecure: true},
		{name: "missing scheme", serverURL: "control.example.com", wantErr: true},
		{name: "unsupported scheme", serverURL: "ftp://control.example.com", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRemoteConfigURL(tt.serverURL, tt.allowInsecure)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateRemoteConfigURL() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestControlPlaneClientRejectsInsecureRedirect(t *testing.T) {
	client := NewControlPlaneHTTPClient(false)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://control.example.com", nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := client.CheckRedirect(req, nil); err == nil {
		t.Fatal("expected HTTP redirect to be rejected")
	}
}
