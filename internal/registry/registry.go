// Package registry is the unified model registry: every model from every
// configured provider, addressable by id or alias regardless of where it runs.
package registry

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dhivadhiva/dl_prism/internal/config"
)

type Entry struct {
	Provider *config.Provider
	Model    *config.Model
}

func (e Entry) HasCapability(cap string) bool {
	for _, c := range e.Model.Capabilities {
		if strings.EqualFold(c, cap) {
			return true
		}
	}
	return false
}

type Registry struct {
	byName  map[string]Entry
	entries []Entry
}

func New(cfg *config.Config) (*Registry, error) {
	r := &Registry{byName: map[string]Entry{}}
	claim := func(name string, e Entry) error {
		key := strings.ToLower(name)
		if prev, ok := r.byName[key]; ok {
			return fmt.Errorf("registry: %q is declared by both %s/%s and %s/%s",
				name, prev.Provider.Name, prev.Model.ID, e.Provider.Name, e.Model.ID)
		}
		r.byName[key] = e
		return nil
	}
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		for j := range p.Models {
			e := Entry{Provider: p, Model: &p.Models[j]}
			r.entries = append(r.entries, e)
			if err := claim(e.Model.ID, e); err != nil {
				return nil, err
			}
			for _, a := range e.Model.Aliases {
				if err := claim(a, e); err != nil {
					return nil, err
				}
			}
		}
	}
	return r, nil
}

// Resolve looks up a model by id or alias (case-insensitive). The special
// form "local:any" or "local:<capability>" selects the first local model,
// optionally filtered by capability.
func (r *Registry) Resolve(name string) (Entry, bool) {
	name = strings.TrimSpace(name)
	if cap, ok := strings.CutPrefix(strings.ToLower(name), "local:"); ok {
		for _, e := range r.entries {
			if !e.Provider.Local {
				continue
			}
			if cap == "any" || e.HasCapability(cap) {
				return e, true
			}
		}
		return Entry{}, false
	}
	e, ok := r.byName[strings.ToLower(name)]
	return e, ok
}

// Entries returns all models in declaration order.
func (r *Registry) Entries() []Entry {
	return r.entries
}

// Names returns every resolvable model name, sorted, for error messages.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.byName))
	for n := range r.byName {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
