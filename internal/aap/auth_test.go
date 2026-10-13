package aap

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aaptui/internal/config"
)

func TestCreateGatewayToken(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		username, password, ok := r.BasicAuth()
		if r.Method != http.MethodPost || r.URL.Path != "/api/gateway/v1/tokens/" || !ok || username != "alice" || password != "private password" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("unexpected token request")
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload["scope"] != "write" || payload["description"] != "aaptui" {
			t.Error("unexpected token payload", err)
		}
		w.WriteHeader(http.StatusCreated)
		if _, err := fmt.Fprint(w, `{"token":"generated-token","scope":"write"}`); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	token, err := CreateToken(context.Background(), config.Settings{URL: server.URL + "/", Mode: config.Gateway, TLSVerify: true}, "alice", "private password", nil)
	if err != nil || token != "generated-token" || requests != 1 {
		t.Fatal("token creation failed", err)
	}
}

func TestTokenCreationSafeFailuresAndNoRetries(t *testing.T) {
	for _, tc := range []struct {
		name, body, message string
		status              int
	}{
		{"unauthorized", "sensitive", "authentication", 401},
		{"forbidden", "sensitive", "permission", 403},
		{"missing", "sensitive", "unsupported", 404},
		{"method", "sensitive", "unsupported", 405},
		{"rejected", "sensitive", "rejected", 400},
		{"rate limited", "sensitive", "uncertain", 429},
		{"server error", "sensitive", "uncertain", 500},
		{"malformed", `{"token":`, "uncertain", 201},
		{"missing token", `{}`, "uncertain", 201},
		{"null", `null`, "uncertain", 201},
		{"newline", `{"token":"sensitive\nsecret"}`, "uncertain", 201},
		{"whitespace", `{"token":"sensitive secret"}`, "uncertain", 201},
		{"oversized", strings.Repeat("sensitive", 10000), "uncertain", 201},
	} {
		t.Run(tc.name, func(t *testing.T) {
			count := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				count++
				w.WriteHeader(tc.status)
				if _, err := fmt.Fprint(w, tc.body); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			token, err := CreateToken(context.Background(), config.Settings{URL: server.URL, Mode: config.Gateway}, "sensitive-user", "sensitive-password", nil)
			if token != "" || err == nil || strings.Contains(err.Error(), "sensitive") || !strings.Contains(err.Error(), tc.message) || count != 1 {
				t.Fatal("unsafe failure or retried token creation", err)
			}
		})
	}
}

func TestTokenCreationRedirectDirectAndTLS(t *testing.T) {
	foreign := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("credentials forwarded to a redirect destination")
	}))
	defer foreign.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, foreign.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	s := config.Settings{URL: server.URL, Mode: config.Gateway}
	if _, err := CreateToken(context.Background(), s, "user", "password", server.Client()); err == nil {
		t.Fatal("redirect accepted")
	}
	s.Mode = config.Direct
	if _, err := CreateToken(context.Background(), s, "user", "password", nil); err == nil || !strings.Contains(err.Error(), "direct-controller") {
		t.Fatal("direct token creation accepted", err)
	}
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := fmt.Fprint(w, `{"token":"tls-token"}`); err != nil {
			t.Error(err)
		}
	}))
	defer tlsServer.Close()
	s = config.Settings{URL: tlsServer.URL, Mode: config.Gateway, TLSVerify: true}
	if _, err := CreateToken(context.Background(), s, "user", "password", nil); err == nil {
		t.Fatal("untrusted certificate accepted")
	}
	s.TLSVerify = false
	if token, err := CreateToken(context.Background(), s, "user", "password", nil); err != nil || token != "tls-token" {
		t.Fatal("explicit TLS override failed", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CreateToken(ctx, s, "user", "password", nil); err == nil {
		t.Fatal("cancelled request succeeded")
	}
}
