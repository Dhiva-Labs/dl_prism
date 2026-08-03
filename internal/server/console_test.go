package server

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dhivadhiva/dl_prism/internal/config"
	"github.com/dhivadhiva/dl_prism/internal/registry"
	"github.com/dhivadhiva/dl_prism/internal/router"
)

// TestSPAServedAtRoot checks that GET / serves the embedded web dashboard.
// It asserts only status and Content-Type, not body content, so it passes
// whether ui/dist holds the placeholder page or the real React build.
func TestSPAServedAtRoot(t *testing.T) {
	cfg, err := config.Parse([]byte(`
providers:
  - name: p
    type: openai
    base_url: http://127.0.0.1:1/v1
    models:
      - id: m1
`))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := registry.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	engine := &router.Engine{Cfg: cfg, Reg: reg, Breaker: router.NewBreaker()}
	srv := httptest.NewServer(New(cfg, reg, engine, nil).Handler())
	defer srv.Close()

	res, err := srv.Client().Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("GET / status = %d, want 200", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
}
