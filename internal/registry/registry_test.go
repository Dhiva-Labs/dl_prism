package registry

import (
	"reflect"
	"sort"
	"testing"

	"github.com/dhivadhiva/dl_prism/internal/config"
)

func twoProviderConfig() *config.Config {
	return &config.Config{
		Providers: []config.Provider{
			{
				Name:    "cloud",
				Type:    "openai",
				BaseURL: "https://api.cloud.test",
				Models: []config.Model{
					{ID: "cloud-model", Aliases: []string{"CM"}, Capabilities: []string{"reasoning"}},
				},
			},
			{
				Name:    "local1",
				Type:    "openai",
				BaseURL: "http://127.0.0.1:1111",
				Local:   true,
				Models: []config.Model{
					{ID: "local-model-1", Aliases: []string{"lm1"}, Capabilities: []string{"reasoning"}},
					{ID: "local-model-2", Aliases: []string{"lm2"}, Capabilities: []string{"code"}},
				},
			},
		},
	}
}

func TestResolveByIDAndAliasCaseInsensitive(t *testing.T) {
	r, err := New(twoProviderConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	e, ok := r.Resolve("cloud-model")
	if !ok || e.Model.ID != "cloud-model" {
		t.Fatalf("Resolve(id) = %+v, %v", e, ok)
	}

	e, ok = r.Resolve("cm")
	if !ok || e.Model.ID != "cloud-model" {
		t.Fatalf("Resolve(lowercase alias) = %+v, %v, want cloud-model", e, ok)
	}

	e, ok = r.Resolve("CLOUD-MODEL")
	if !ok || e.Model.ID != "cloud-model" {
		t.Fatalf("Resolve(uppercase id) = %+v, %v, want cloud-model", e, ok)
	}

	e, ok = r.Resolve("Cm")
	if !ok || e.Model.ID != "cloud-model" {
		t.Fatalf("Resolve(mixed-case alias) = %+v, %v, want cloud-model", e, ok)
	}
}

func TestResolveUnknownName(t *testing.T) {
	r, err := New(twoProviderConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := r.Resolve("does-not-exist"); ok {
		t.Fatalf("Resolve(unknown) ok = true, want false")
	}
}

func TestNewDuplicateAliasAcrossProvidersErrors(t *testing.T) {
	cfg := &config.Config{
		Providers: []config.Provider{
			{
				Name:    "p1",
				Type:    "openai",
				BaseURL: "https://api.p1.test",
				Models: []config.Model{
					{ID: "model-a", Aliases: []string{"shared"}},
				},
			},
			{
				Name:    "p2",
				Type:    "openai",
				BaseURL: "https://api.p2.test",
				Models: []config.Model{
					{ID: "model-b", Aliases: []string{"shared"}},
				},
			},
		},
	}
	if _, err := New(cfg); err == nil {
		t.Fatal("New() error = nil, want error for duplicate alias across providers")
	}
}

func TestNewDuplicateIDAcrossProvidersErrors(t *testing.T) {
	cfg := &config.Config{
		Providers: []config.Provider{
			{
				Name:    "p1",
				Type:    "openai",
				BaseURL: "https://api.p1.test",
				Models: []config.Model{
					{ID: "same-id"},
				},
			},
			{
				Name:    "p2",
				Type:    "openai",
				BaseURL: "https://api.p2.test",
				Models: []config.Model{
					{ID: "same-id"},
				},
			},
		},
	}
	if _, err := New(cfg); err == nil {
		t.Fatal("New() error = nil, want error for duplicate model id across providers")
	}
}

func TestResolveLocalAnyPicksFirstLocalModel(t *testing.T) {
	r, err := New(twoProviderConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	e, ok := r.Resolve("local:any")
	if !ok {
		t.Fatal("Resolve(local:any) ok = false, want true")
	}
	if e.Model.ID != "local-model-1" {
		t.Errorf("Resolve(local:any) = %q, want first local model %q", e.Model.ID, "local-model-1")
	}
}

func TestResolveLocalWithCapabilityFilter(t *testing.T) {
	r, err := New(twoProviderConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	e, ok := r.Resolve("local:code")
	if !ok {
		t.Fatal("Resolve(local:code) ok = false, want true")
	}
	if e.Model.ID != "local-model-2" {
		t.Errorf("Resolve(local:code) = %q, want %q (the local model with code capability)", e.Model.ID, "local-model-2")
	}
}

func TestResolveLocalAnyNoLocalProvidersFails(t *testing.T) {
	cfg := &config.Config{
		Providers: []config.Provider{
			{
				Name:    "cloud",
				Type:    "openai",
				BaseURL: "https://api.cloud.test",
				Models: []config.Model{
					{ID: "cloud-model"},
				},
			},
		},
	}
	r, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := r.Resolve("local:any"); ok {
		t.Fatal("Resolve(local:any) ok = true, want false when no local providers configured")
	}
}

func TestNamesSorted(t *testing.T) {
	r, err := New(twoProviderConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	names := r.Names()
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	if !reflect.DeepEqual(names, sorted) {
		t.Errorf("Names() = %v, not sorted (want %v)", names, sorted)
	}
	// Should include both ids and aliases, lowercased.
	want := []string{"cloud-model", "cm", "lm1", "lm2", "local-model-1", "local-model-2"}
	sort.Strings(want)
	if !reflect.DeepEqual(names, want) {
		t.Errorf("Names() = %v, want %v", names, want)
	}
}
