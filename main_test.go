package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
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
