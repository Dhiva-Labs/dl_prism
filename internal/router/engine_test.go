package router

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/dhivadhiva/dl_prism/internal/config"
	"github.com/dhivadhiva/dl_prism/internal/provider"
	"github.com/dhivadhiva/dl_prism/internal/registry"
)

// baseConfig has two providers: a cloud one (large context window) and a
// local one (small context window), plus one auto-routing rule for python.
func baseConfig() *config.Config {
	return &config.Config{
		Providers: []config.Provider{
			{
				Name:    "cloud",
				Type:    "anthropic",
				BaseURL: "https://api.cloud.test",
				Models: []config.Model{
					{ID: "cloud-big", Aliases: []string{"big"}, ContextWindow: 200000},
				},
			},
			{
				Name:    "local",
				Type:    "openai",
				BaseURL: "http://127.0.0.1:11434/v1",
				Local:   true,
				Models: []config.Model{
					{ID: "local-small", Aliases: []string{"small"}, ContextWindow: 50},
				},
			},
		},
		Routing: config.Routing{
			Default: "cloud-big",
			Rules: []config.Rule{
				{If: config.Condition{Language: "python"}, Then: "local-small"},
			},
		},
	}
}

func newEngine(t *testing.T, cfg *config.Config) *Engine {
	t.Helper()
	reg, err := registry.New(cfg)
	if err != nil {
		t.Fatalf("registry.New: %v", err)
	}
	return &Engine{Cfg: cfg, Reg: reg}
}

func textReq(t *testing.T, model, text string) *provider.ChatRequest {
	t.Helper()
	b, err := json.Marshal(text)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return &provider.ChatRequest{
		Model:    model,
		Messages: []provider.Message{{Role: "user", Content: b}},
	}
}

func chainIDs(chain []registry.Entry) []string {
	ids := make([]string, len(chain))
	for i, e := range chain {
		ids[i] = e.Model.ID
	}
	return ids
}

func TestEngineManualModeResolvesAlias(t *testing.T) {
	e := newEngine(t, baseConfig())
	dec, err := e.Route(textReq(t, "big", "hello"))
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if dec.Mode != "manual" {
		t.Errorf("Mode = %q, want %q", dec.Mode, "manual")
	}
	if len(dec.Chain) == 0 || dec.Chain[0].Model.ID != "cloud-big" {
		t.Errorf("Chain = %v, want first entry cloud-big", chainIDs(dec.Chain))
	}
}

func TestEngineAutoModePythonRuleMatch(t *testing.T) {
	e := newEngine(t, baseConfig())
	req := textReq(t, "", "```python\ndef foo():\n    pass\n```")
	dec, err := e.Route(req)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if dec.Mode != "auto" {
		t.Errorf("Mode = %q, want %q", dec.Mode, "auto")
	}
	if len(dec.Chain) == 0 || dec.Chain[0].Model.ID != "local-small" {
		t.Errorf("Chain = %v, want first entry local-small (matched {language: python} rule)", chainIDs(dec.Chain))
	}
	if !strings.Contains(dec.Reason, "rule 1 matched") {
		t.Errorf("Reason = %q, want it to mention rule 1", dec.Reason)
	}
}

func TestEngineFallbackChainDedupeAndOrder(t *testing.T) {
	cfg := baseConfig()
	cfg.Routing.Fallbacks = map[string][]string{
		// "cloud-big" (dup of primary), "local-small" (dup), "big" (alias of primary).
		"cloud-big": {"cloud-big", "local-small", "local-small", "big"},
	}
	e := newEngine(t, cfg)
	dec, err := e.Route(textReq(t, "cloud-big", "hi"))
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	want := []string{"cloud-big", "local-small"}
	if got := chainIDs(dec.Chain); !reflect.DeepEqual(got, want) {
		t.Errorf("Chain = %v, want %v (deduped, in order)", got, want)
	}
}

func TestEngineOfflineFiltersChainToLocalOnly(t *testing.T) {
	cfg := baseConfig()
	cfg.Offline = true
	cfg.Routing.Fallbacks = map[string][]string{
		"cloud-big": {"local-small"},
	}
	e := newEngine(t, cfg)
	dec, err := e.Route(textReq(t, "cloud-big", "hi"))
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	want := []string{"local-small"}
	if got := chainIDs(dec.Chain); !reflect.DeepEqual(got, want) {
		t.Errorf("Chain = %v, want %v (non-local primary dropped)", got, want)
	}
}

func TestEngineOfflineErrorsWhenNothingLocal(t *testing.T) {
	cfg := baseConfig()
	cfg.Offline = true
	// No fallbacks configured, so the only candidate is the non-local primary.
	e := newEngine(t, cfg)
	_, err := e.Route(textReq(t, "cloud-big", "hi"))
	if err == nil {
		t.Fatal("Route() error = nil, want error when offline with no local candidate")
	}
	if !strings.Contains(err.Error(), "offline mode") {
		t.Errorf("Route() error = %v, want it to mention offline mode", err)
	}
}

func TestEngineAutoModeContextWindowFilterDropsTooSmall(t *testing.T) {
	cfg := baseConfig()
	cfg.Routing.Fallbacks = map[string][]string{
		"local-small": {"cloud-big"},
	}
	e := newEngine(t, cfg)
	// Long enough that EstTokens exceeds local-small's 50-token context window.
	longText := "```python\n" + strings.Repeat("def foo():\n    pass\n", 20) + "```"
	dec, err := e.Route(textReq(t, "", longText))
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	want := []string{"cloud-big"}
	if got := chainIDs(dec.Chain); !reflect.DeepEqual(got, want) {
		t.Errorf("Chain = %v, want %v (local-small dropped: too small for estimated tokens)", got, want)
	}
}

func TestEngineManualModeIgnoresContextWindow(t *testing.T) {
	e := newEngine(t, baseConfig())
	// Longer than local-small's 50-token context window, but manual picks
	// are the user's business and must not be filtered.
	longText := strings.Repeat("x", 1000)
	dec, err := e.Route(textReq(t, "local-small", longText))
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	want := []string{"local-small"}
	if got := chainIDs(dec.Chain); !reflect.DeepEqual(got, want) {
		t.Errorf("Chain = %v, want %v (manual mode should not context-filter)", got, want)
	}
}

func TestEngineUnknownModelReturnsUnknownModelError(t *testing.T) {
	e := newEngine(t, baseConfig())
	_, err := e.Route(textReq(t, "does-not-exist", "hi"))
	var umErr *UnknownModelError
	if !errors.As(err, &umErr) {
		t.Fatalf("Route() error = %v (%T), want *UnknownModelError", err, err)
	}
	if umErr.Requested != "does-not-exist" {
		t.Errorf("Requested = %q, want %q", umErr.Requested, "does-not-exist")
	}
}

func TestEngineValidateCatchesRuleWithUnknownModel(t *testing.T) {
	cfg := baseConfig()
	cfg.Routing.Rules = append(cfg.Routing.Rules, config.Rule{
		If:   config.Condition{Task: "docs"},
		Then: "nonexistent-model",
	})
	e := newEngine(t, cfg)
	if err := e.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want error for rule referencing unknown model")
	}
}

func TestEngineValidatePassesForBaseConfig(t *testing.T) {
	e := newEngine(t, baseConfig())
	if err := e.Validate(); err != nil {
		t.Errorf("Validate() error = %v, want nil for a valid config", err)
	}
}

func TestEnginePipelinePrefixNotImplemented(t *testing.T) {
	e := newEngine(t, baseConfig())
	_, err := e.Route(textReq(t, "pipeline:foo", "hi"))
	if err == nil {
		t.Fatal("Route() error = nil, want not-implemented error for pipeline: model")
	}
	if !strings.Contains(err.Error(), "not implemented") {
		t.Errorf("Route() error = %v, want it to mention not implemented", err)
	}
}
