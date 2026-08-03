// Package config loads and validates prism.yaml.
package config

import (
	"fmt"
	"net"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const DefaultListen = "127.0.0.1:8130"

type Config struct {
	Server    Server     `yaml:"server"`
	Providers []Provider `yaml:"providers"`
	Routing   Routing    `yaml:"routing"`
	Budget    Budget     `yaml:"budget"`
	// Offline restricts routing to local providers only. Also settable
	// with `prism serve --offline`.
	Offline bool `yaml:"offline"`
}

type Server struct {
	Listen string `yaml:"listen"`
	// APIKeys, when non-empty, are bearer tokens clients must present.
	// Required when Listen is not a loopback address.
	APIKeys []string `yaml:"api_keys"`
}

type Provider struct {
	Name    string `yaml:"name"`
	Type    string `yaml:"type"` // "openai" (any OpenAI-compatible API) or "anthropic"
	BaseURL string `yaml:"base_url"`
	// APIKey may embed ${ENV_VAR} references; APIKeyEnv names a variable
	// to read directly. Keys never appear in logs or API responses.
	APIKey    string            `yaml:"api_key"`
	APIKeyEnv string            `yaml:"api_key_env"`
	Headers   map[string]string `yaml:"headers"`
	Local     bool              `yaml:"local"`
	Models    []Model           `yaml:"models"`
}

// Key resolves the API key for this provider, or "" for keyless (local) endpoints.
func (p *Provider) Key() string {
	if p.APIKey != "" {
		return os.ExpandEnv(p.APIKey)
	}
	if p.APIKeyEnv != "" {
		return os.Getenv(p.APIKeyEnv)
	}
	return ""
}

type Model struct {
	ID            string   `yaml:"id"`
	Aliases       []string `yaml:"aliases"`
	ContextWindow int      `yaml:"context_window"`
	Capabilities  []string `yaml:"capabilities"`
	Speed         string   `yaml:"speed"`
	Cost          Cost     `yaml:"cost"`
}

// Cost is USD per million tokens. Zero for local models.
type Cost struct {
	InputPerMTok  float64 `yaml:"input_per_mtok"`
	OutputPerMTok float64 `yaml:"output_per_mtok"`
}

type Routing struct {
	// Default is used when no rule matches in automatic mode.
	Default string `yaml:"default"`
	// Rules are evaluated top to bottom; the first full match wins.
	Rules []Rule `yaml:"rules"`
	// Fallbacks maps a model name/alias to the ordered failover chain
	// tried when that model's provider fails.
	Fallbacks map[string][]string `yaml:"fallbacks"`
}

type Rule struct {
	If   Condition `yaml:"if"`
	Then string    `yaml:"then"`
}

// Condition fields are ANDed; unset fields match anything. An empty
// condition matches every request (useful as a catch-all last rule).
type Condition struct {
	Task             string `yaml:"task"`     // code | security | docs | vision | general
	Language         string `yaml:"language"` // programming language, e.g. "python"
	Vision           *bool  `yaml:"vision"`
	Offline          *bool  `yaml:"offline"`
	BudgetExceeded   *bool  `yaml:"budget_exceeded"`
	MinContextTokens int    `yaml:"min_context_tokens"`
}

// Describe renders the set fields for routing transparency messages.
func (c Condition) Describe() string {
	var parts []string
	if c.Task != "" {
		parts = append(parts, "task="+c.Task)
	}
	if c.Language != "" {
		parts = append(parts, "language="+c.Language)
	}
	if c.Vision != nil {
		parts = append(parts, fmt.Sprintf("vision=%t", *c.Vision))
	}
	if c.Offline != nil {
		parts = append(parts, fmt.Sprintf("offline=%t", *c.Offline))
	}
	if c.BudgetExceeded != nil {
		parts = append(parts, fmt.Sprintf("budget_exceeded=%t", *c.BudgetExceeded))
	}
	if c.MinContextTokens > 0 {
		parts = append(parts, fmt.Sprintf("min_context_tokens=%d", c.MinContextTokens))
	}
	if len(parts) == 0 {
		return "always"
	}
	return strings.Join(parts, " ")
}

type Budget struct {
	MonthlyUSD float64 `yaml:"monthly_usd"`
}

func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(raw)
}

func Parse(raw []byte) (*Config, error) {
	var cfg Config
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if cfg.Server.Listen == "" {
		cfg.Server.Listen = DefaultListen
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) validate() error {
	if len(c.Providers) == 0 {
		return fmt.Errorf("config: at least one provider is required")
	}
	seen := map[string]bool{}
	for i := range c.Providers {
		p := &c.Providers[i]
		if p.Name == "" {
			return fmt.Errorf("config: provider %d has no name", i)
		}
		if seen[p.Name] {
			return fmt.Errorf("config: duplicate provider name %q", p.Name)
		}
		seen[p.Name] = true
		if p.Type != "openai" && p.Type != "anthropic" {
			return fmt.Errorf("config: provider %q: unknown type %q (want openai or anthropic)", p.Name, p.Type)
		}
		if !strings.HasPrefix(p.BaseURL, "http://") && !strings.HasPrefix(p.BaseURL, "https://") {
			return fmt.Errorf("config: provider %q: base_url must start with http:// or https://", p.Name)
		}
		if len(p.Models) == 0 {
			return fmt.Errorf("config: provider %q declares no models", p.Name)
		}
		for j := range p.Models {
			if p.Models[j].ID == "" {
				return fmt.Errorf("config: provider %q: model %d has no id", p.Name, j)
			}
		}
	}
	for i, r := range c.Routing.Rules {
		if r.Then == "" {
			return fmt.Errorf("config: routing rule %d has no 'then' target", i)
		}
	}
	if !isLoopback(c.Server.Listen) && len(c.Server.APIKeys) == 0 {
		return fmt.Errorf("config: server.api_keys is required when listening on non-loopback address %q", c.Server.Listen)
	}
	return nil
}

func isLoopback(listen string) bool {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		host = listen
	}
	switch host {
	case "localhost", "::1":
		return true
	case "":
		// ":8130" and "" bind all interfaces, not loopback.
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
