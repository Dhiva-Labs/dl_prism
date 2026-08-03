package config

import (
	"strings"
	"testing"
)

const fullExampleYAML = `
server:
  listen: 127.0.0.1:8130
  api_keys: []

providers:
  - name: anthropic
    type: anthropic
    base_url: https://api.anthropic.com
    api_key_env: ANTHROPIC_API_KEY
    models:
      - id: claude-sonnet-4-5
        aliases: [claude-sonnet]
        context_window: 200000
        capabilities: [code, reasoning, vision, tools]
        speed: fast
        cost: {input_per_mtok: 3.00, output_per_mtok: 15.00}

  - name: openai
    type: openai
    base_url: https://api.openai.com/v1
    api_key_env: OPENAI_API_KEY
    models:
      - id: gpt-5
        context_window: 400000
        capabilities: [code, reasoning, vision, tools]
        speed: medium
        cost: {input_per_mtok: 1.25, output_per_mtok: 10.00}

  - name: ollama
    type: openai
    base_url: http://127.0.0.1:11434/v1
    local: true
    models:
      - id: qwen2.5-coder:14b
        aliases: [qwen-coder]
        context_window: 32768
        capabilities: [code]
        speed: fast

routing:
  default: claude-sonnet
  rules:
    - if: {language: python}
      then: qwen-coder
    - if: {task: security}
      then: claude-sonnet
    - if: {offline: true}
      then: local:any
  fallbacks:
    claude-sonnet: [gpt-5, qwen-coder]
    gpt-5: [claude-sonnet]

budget:
  monthly_usd: 50
`

func TestParseValidFullYAML(t *testing.T) {
	cfg, err := Parse([]byte(fullExampleYAML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Server.Listen != "127.0.0.1:8130" {
		t.Errorf("Server.Listen = %q, want %q", cfg.Server.Listen, "127.0.0.1:8130")
	}
	if len(cfg.Providers) != 3 {
		t.Fatalf("len(Providers) = %d, want 3", len(cfg.Providers))
	}
	if cfg.Providers[0].Name != "anthropic" || cfg.Providers[0].Models[0].ID != "claude-sonnet-4-5" {
		t.Errorf("unexpected first provider: %+v", cfg.Providers[0])
	}
	if !cfg.Providers[2].Local {
		t.Errorf("expected ollama provider to be local")
	}
	if cfg.Routing.Default != "claude-sonnet" {
		t.Errorf("Routing.Default = %q, want %q", cfg.Routing.Default, "claude-sonnet")
	}
	if len(cfg.Routing.Rules) != 3 {
		t.Fatalf("len(Routing.Rules) = %d, want 3", len(cfg.Routing.Rules))
	}
	if got := cfg.Routing.Fallbacks["claude-sonnet"]; len(got) != 2 || got[0] != "gpt-5" {
		t.Errorf("Routing.Fallbacks[claude-sonnet] = %v, want [gpt-5 qwen-coder]", got)
	}
	if cfg.Budget.MonthlyUSD != 50 {
		t.Errorf("Budget.MonthlyUSD = %v, want 50", cfg.Budget.MonthlyUSD)
	}
}

func TestParseDefaultsListen(t *testing.T) {
	yaml := `
providers:
  - name: p
    type: openai
    base_url: https://api.example.com
    models:
      - id: m1
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Server.Listen != DefaultListen {
		t.Errorf("Server.Listen = %q, want default %q", cfg.Server.Listen, DefaultListen)
	}
}

func TestProviderKeyViaEnvVar(t *testing.T) {
	t.Setenv("PRISM_TEST_KEY", "secret-123")
	p := Provider{APIKeyEnv: "PRISM_TEST_KEY"}
	if got := p.Key(); got != "secret-123" {
		t.Errorf("Key() = %q, want %q", got, "secret-123")
	}
}

func TestProviderKeyExpandsVarInAPIKey(t *testing.T) {
	t.Setenv("PRISM_TEST_TOKEN", "tok-456")
	p := Provider{APIKey: "prefix-${PRISM_TEST_TOKEN}-suffix"}
	want := "prefix-tok-456-suffix"
	if got := p.Key(); got != want {
		t.Errorf("Key() = %q, want %q", got, want)
	}
}

func TestProviderKeyEmptyWhenUnset(t *testing.T) {
	p := Provider{}
	if got := p.Key(); got != "" {
		t.Errorf("Key() = %q, want empty", got)
	}
}

func TestProviderKeyPrefersAPIKeyOverEnv(t *testing.T) {
	t.Setenv("PRISM_TEST_UNUSED", "should-not-be-used")
	p := Provider{APIKey: "literal-key", APIKeyEnv: "PRISM_TEST_UNUSED"}
	if got := p.Key(); got != "literal-key" {
		t.Errorf("Key() = %q, want %q", got, "literal-key")
	}
}

func TestValidateErrors(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "no providers",
			yaml: `
server:
  listen: 127.0.0.1:9999
providers: []
`,
			wantErr: "at least one provider is required",
		},
		{
			name: "duplicate provider name",
			yaml: `
providers:
  - name: dup
    type: openai
    base_url: https://api.test.com
    models:
      - id: model-1
  - name: dup
    type: openai
    base_url: https://api.test.com
    models:
      - id: model-2
`,
			wantErr: `duplicate provider name "dup"`,
		},
		{
			name: "bad type",
			yaml: `
providers:
  - name: test
    type: bogus
    base_url: https://api.test.com
    models:
      - id: model-1
`,
			wantErr: "unknown type",
		},
		{
			name: "bad base_url",
			yaml: `
providers:
  - name: test
    type: openai
    base_url: ftp://api.test.com
    models:
      - id: model-1
`,
			wantErr: "base_url must start with",
		},
		{
			name: "provider with no models",
			yaml: `
providers:
  - name: test
    type: openai
    base_url: https://api.test.com
    models: []
`,
			wantErr: "declares no models",
		},
		{
			name: "rule with empty then",
			yaml: `
providers:
  - name: test
    type: openai
    base_url: https://api.test.com
    models:
      - id: model-1
routing:
  rules:
    - if: {task: code}
      then: ""
`,
			wantErr: "has no 'then' target",
		},
		{
			name: "non-loopback listen without api_keys",
			yaml: `
server:
  listen: 0.0.0.0:8130
providers:
  - name: test
    type: openai
    base_url: https://api.test.com
    models:
      - id: model-1
`,
			wantErr: "server.api_keys is required",
		},
		{
			// ":8130" binds all interfaces in Go, not loopback.
			name: "bare-port listen without api_keys",
			yaml: `
server:
  listen: ":8130"
providers:
  - name: test
    type: openai
    base_url: https://api.test.com
    models:
      - id: model-1
`,
			wantErr: "server.api_keys is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.yaml))
			if err == nil {
				t.Fatalf("Parse() error = nil, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Parse() error = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestValidateLoopbackWithoutAPIKeysIsFine(t *testing.T) {
	yaml := `
server:
  listen: 127.0.0.1:8130
providers:
  - name: test
    type: openai
    base_url: https://api.test.com
    models:
      - id: model-1
`
	if _, err := Parse([]byte(yaml)); err != nil {
		t.Errorf("Parse() error = %v, want nil for loopback listener without api_keys", err)
	}
}

func TestParseRejectsUnknownField(t *testing.T) {
	yaml := `
server:
  listen: 127.0.0.1:8130
providers:
  - name: test
    type: openai
    base_url: https://api.test.com
    models:
      - id: model-1
unknown_top_level_field: true
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("Parse() error = nil, want error for unknown field")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Parse() error = %v, want it to mention the field was not found", err)
	}
}
