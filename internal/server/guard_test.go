package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dhivadhiva/dl_prism/internal/config"
)

const chatBody = `{"model":"gpt-test","messages":[{"role":"user","content":"hi"}]}`

// send issues a request with an explicit Host, which the fixture helpers
// otherwise pin to the loopback.
func send(f *fixture, method, path, host, body string, headers ...[2]string) *httptest.ResponseRecorder {
	f.t.Helper()
	var r *strings.Reader
	if body != "" {
		r = strings.NewReader(body)
	} else {
		r = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, r)
	req.Host = host
	if body != "" {
		// What a cross-site form or fetch can send without a preflight.
		req.Header.Set("Content-Type", "text/plain")
	}
	for _, h := range headers {
		req.Header.Set(h[0], h[1])
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, req)
	return w
}

// A keyless gateway is the shipped default. A web page the user visits
// must not be able to reach it through their browser.
func TestGuardRefusesCrossSiteRequestsWithoutKeys(t *testing.T) {
	for _, origin := range []string{"https://evil.example", "http://evil.example:8130", "null", "http://127.0.0.1:9999"} {
		t.Run(origin, func(t *testing.T) {
			f := newFixture(t)
			w := send(f, http.MethodPost, "/v1/chat/completions", testHost, chatBody, [2]string{"Origin", origin})
			if w.Code != http.StatusForbidden {
				t.Errorf("status = %d, want 403 (body %s)", w.Code, w.Body.String())
			}
			if f.primary.Hits()+f.backup.Hits() != 0 {
				t.Error("a cross-site request reached an upstream")
			}
		})
	}

	t.Run("workbench mutations too", func(t *testing.T) {
		f := newFixture(t)
		w := send(f, http.MethodPost, "/admin/projects", testHost,
			`{"name":"x","settings":{"agent_enabled":true,"approval_mode":"never"}}`,
			[2]string{"Origin", "https://evil.example"})
		if w.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403 (body %s)", w.Code, w.Body.String())
		}
	})
}

func TestGuardAllowsOwnOriginAndExtensions(t *testing.T) {
	for _, origin := range []string{
		"http://127.0.0.1:8130",
		"HTTP://127.0.0.1:8130",
		"chrome-extension://abcdefghijklmnopabcdefghijklmnop",
		"moz-extension://0c2d3a6e-9d4b-4a0f-8e6b-1f2a3b4c5d6e",
	} {
		t.Run(origin, func(t *testing.T) {
			f := newFixture(t)
			w := send(f, http.MethodGet, "/v1/models", testHost, "", [2]string{"Origin", origin})
			if w.Code != http.StatusOK {
				t.Errorf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
			}
		})
	}
	t.Run("no Origin header at all", func(t *testing.T) {
		f := newFixture(t)
		if w := send(f, http.MethodGet, "/v1/models", testHost, ""); w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", w.Code)
		}
	})
}

// DNS rebinding makes the attacker's page same-origin with the gateway;
// the giveaway is a Host header naming a domain the gateway never served.
func TestGuardRefusesForeignHostWithoutKeys(t *testing.T) {
	for _, host := range []string{"evil.example:8130", "evil.example", "127.0.0.1.evil.example:8130", "localhost.evil.example", ""} {
		t.Run(host, func(t *testing.T) {
			f := newFixture(t)
			w := send(f, http.MethodGet, "/v1/models", host, "")
			if w.Code != http.StatusForbidden {
				t.Errorf("status = %d, want 403 (body %s)", w.Code, w.Body.String())
			}
		})
	}
	t.Run("the SPA is not served to a rebound host either", func(t *testing.T) {
		f := newFixture(t)
		if w := send(f, http.MethodGet, "/", "evil.example:8130", ""); w.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403", w.Code)
		}
	})
}

func TestGuardAcceptsLoopbackHosts(t *testing.T) {
	for _, host := range []string{"127.0.0.1:8130", "127.0.0.1", "127.0.0.2:8130", "localhost:8130", "LocalHost", "[::1]:8130", "app.localhost:8130", "localhost.:8130"} {
		t.Run(host, func(t *testing.T) {
			f := newFixture(t)
			if w := send(f, http.MethodGet, "/v1/models", host, ""); w.Code != http.StatusOK {
				t.Errorf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
			}
		})
	}
	t.Run("the configured listen address", func(t *testing.T) {
		f := newFixture(t, func(cfg *config.Config) { cfg.Server.Listen = "192.0.2.10:8130" })
		if w := send(f, http.MethodGet, "/v1/models", "192.0.2.10:8130", ""); w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
		}
	})
}

// With api_keys configured the bearer token is the defence, and a reverse
// proxy may legitimately present any Host or Origin.
func TestGuardStepsAsideWhenKeysAreConfigured(t *testing.T) {
	withKeys := func(cfg *config.Config) { cfg.Server.APIKeys = []string{"sk-prism-test"} }
	bearer := [2]string{"Authorization", "Bearer sk-prism-test"}
	foreign := [2]string{"Origin", "https://prism.example"}

	f := newFixture(t, withKeys)
	if w := send(f, http.MethodGet, "/v1/models", "prism.example", "", bearer, foreign); w.Code != http.StatusOK {
		t.Errorf("authenticated proxied request: status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	if w := send(f, http.MethodPost, "/v1/chat/completions", "prism.example", chatBody, foreign); w.Code != http.StatusUnauthorized {
		t.Errorf("cross-site request without the key: status = %d, want 401", w.Code)
	}
	if f.primary.Hits()+f.backup.Hits() != 0 {
		t.Error("an unauthenticated request reached an upstream")
	}
}
