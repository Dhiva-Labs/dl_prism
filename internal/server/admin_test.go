package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dhivadhiva/dl_prism/internal/config"
)

// --- /admin/status ---------------------------------------------------

type statusProvider struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Local   bool   `json:"local"`
	Healthy bool   `json:"healthy"`
	Models  int    `json:"models"`
}

type statusResponse struct {
	Version       string           `json:"version"`
	UptimeSeconds int              `json:"uptime_seconds"`
	Offline       bool             `json:"offline"`
	Listen        string           `json:"listen"`
	Providers     []statusProvider `json:"providers"`
}

func TestAdminStatusShape(t *testing.T) {
	f := newFixture(t)
	f.srv.Version = "9.9.9-test"

	w := f.get("/admin/status")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var out statusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding /admin/status: %v (%s)", err, w.Body.String())
	}
	if out.Version != "9.9.9-test" {
		t.Errorf("version = %q, want 9.9.9-test", out.Version)
	}
	if out.UptimeSeconds < 0 {
		t.Errorf("uptime_seconds = %d, want >= 0", out.UptimeSeconds)
	}
	if out.Offline {
		t.Errorf("offline = %v, want false (fixture config is online)", out.Offline)
	}
	if out.Listen != f.cfg.Server.Listen {
		t.Errorf("listen = %q, want %q", out.Listen, f.cfg.Server.Listen)
	}

	byName := map[string]statusProvider{}
	for _, p := range out.Providers {
		byName[p.Name] = p
	}
	if len(out.Providers) != 3 {
		t.Fatalf("providers = %+v, want 3 entries", out.Providers)
	}
	primary, ok := byName["primary"]
	if !ok {
		t.Fatalf("providers missing primary; got %+v", out.Providers)
	}
	if primary.Type != "openai" {
		t.Errorf("primary type = %q, want openai", primary.Type)
	}
	if primary.Local {
		t.Errorf("primary local = %v, want false", primary.Local)
	}
	if primary.Models != 1 {
		t.Errorf("primary models = %d, want 1", primary.Models)
	}
	if !primary.Healthy {
		t.Errorf("primary healthy = %v, want true before any failures", primary.Healthy)
	}
	anthro, ok := byName["anthro"]
	if !ok {
		t.Fatalf("providers missing anthro; got %+v", out.Providers)
	}
	if anthro.Type != "anthropic" {
		t.Errorf("anthro type = %q, want anthropic", anthro.Type)
	}
}

// The breaker demotes a provider after 3 consecutive failures; /admin/status
// must reflect that as healthy=false, while untouched providers stay healthy.
func TestAdminStatusHealthyFlipsAfterBreakerFailures(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 3; i++ {
		f.engine.Breaker.Failure("primary")
	}

	w := f.get("/admin/status")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	var out statusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding /admin/status: %v (%s)", err, w.Body.String())
	}
	byName := map[string]statusProvider{}
	for _, p := range out.Providers {
		byName[p.Name] = p
	}
	if byName["primary"].Healthy {
		t.Error("primary healthy = true, want false after 3 consecutive breaker failures")
	}
	if !byName["backup"].Healthy {
		t.Error("backup healthy = false, want true: its breaker was never tripped")
	}
	if !byName["anthro"].Healthy {
		t.Error("anthro healthy = false, want true: its breaker was never tripped")
	}
}

// --- /admin/routing ----------------------------------------------------

type routingResponse struct {
	Default   string              `json:"default"`
	Rules     []routingRule       `json:"rules"`
	Fallbacks map[string][]string `json:"fallbacks"`
}

func TestAdminRouting(t *testing.T) {
	f := newFixture(t)
	w := f.get("/admin/routing")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}

	var out routingResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding /admin/routing: %v (%s)", err, w.Body.String())
	}
	if out.Default != "backup-test" {
		t.Errorf("default = %q, want backup-test", out.Default)
	}
	if len(out.Rules) != 1 {
		t.Fatalf("rules = %+v, want 1 rule", out.Rules)
	}
	// The condition is rendered through Condition.Describe(), not the raw struct.
	if out.Rules[0].If != "task=code" {
		t.Errorf("rules[0].if = %q, want the Describe() rendering %q", out.Rules[0].If, "task=code")
	}
	if out.Rules[0].Then != "gpt-test" {
		t.Errorf("rules[0].then = %q, want gpt-test", out.Rules[0].Then)
	}
	if chain := out.Fallbacks["gpt-test"]; len(chain) != 1 || chain[0] != "backup-test" {
		t.Errorf("fallbacks[gpt-test] = %v, want [backup-test]", chain)
	}
}

// Empty rules/fallbacks must serialize as [] and {}, never null, so the UI
// doesn't have to special-case a missing field.
func TestAdminRoutingEmptyRulesAndFallbacksSerializeAsEmptyContainers(t *testing.T) {
	empty := func(cfg *config.Config) {
		cfg.Routing.Rules = nil
		cfg.Routing.Fallbacks = nil
	}
	f := newFixture(t, empty)

	w := f.get("/admin/routing")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"rules":[]`) {
		t.Errorf("body = %s, want \"rules\":[] for a config with no rules", body)
	}
	if !strings.Contains(body, `"fallbacks":{}`) {
		t.Errorf("body = %s, want \"fallbacks\":{} for a config with no fallbacks", body)
	}

	var out routingResponse
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decoding /admin/routing: %v (%s)", err, body)
	}
	if len(out.Rules) != 0 {
		t.Errorf("rules = %+v, want empty", out.Rules)
	}
	if len(out.Fallbacks) != 0 {
		t.Errorf("fallbacks = %+v, want empty", out.Fallbacks)
	}
}

// --- /admin/usage --------------------------------------------------------

func TestAdminUsageIncludesDaily(t *testing.T) {
	f := newFixture(t)
	f.primary.respond(http.StatusOK, okCompletion)
	f.post("/v1/chat/completions", `{"model":"gpt-test","messages":[{"role":"user","content":"hi"}]}`)

	w := f.get("/admin/usage")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	var out struct {
		Daily []struct {
			Date     string  `json:"date"`
			CostUSD  float64 `json:"cost_usd"`
			Requests int     `json:"requests"`
		} `json:"daily"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding /admin/usage: %v (%s)", err, w.Body.String())
	}
	if len(out.Daily) != 1 {
		t.Fatalf("daily = %+v, want 1 day of usage", out.Daily)
	}
	if out.Daily[0].Requests != 1 {
		t.Errorf("daily[0].requests = %d, want 1", out.Daily[0].Requests)
	}
	if out.Daily[0].CostUSD <= 0 {
		t.Errorf("daily[0].cost_usd = %v, want > 0", out.Daily[0].CostUSD)
	}
}

// A Server built without a ledger must still answer with an empty (not null)
// daily series.
func TestAdminUsageDailyWithoutLedgerIsEmptyNotNull(t *testing.T) {
	f := newFixture(t)
	srv := New(f.cfg, f.reg, f.engine, nil)
	req := httptest.NewRequest(http.MethodGet, "/admin/usage", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"daily":[]`) {
		t.Errorf("body = %s, want \"daily\":[] when there is no ledger", body)
	}
}

// --- auth ----------------------------------------------------------------

// All three admin endpoints must sit behind the same bearer-key auth as the
// rest of the API when api_keys are configured.
func TestAdminEndpointsRequireAuth(t *testing.T) {
	withKeys := func(cfg *config.Config) { cfg.Server.APIKeys = []string{"sk-prism-test"} }
	for _, path := range []string{"/admin/status", "/admin/routing", "/admin/usage"} {
		t.Run(path, func(t *testing.T) {
			f := newFixture(t, withKeys)
			if w := f.get(path); w.Code != http.StatusUnauthorized {
				t.Errorf("unauthenticated GET %s status = %d, want 401", path, w.Code)
			}
			if w := f.get(path, [2]string{"Authorization", "Bearer sk-prism-test"}); w.Code != http.StatusOK {
				t.Errorf("authenticated GET %s status = %d, want 200 (body %s)", path, w.Code, w.Body.String())
			}
		})
	}
}
