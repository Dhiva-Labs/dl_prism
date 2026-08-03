package router

import (
	"fmt"
	"strings"

	"github.com/dhivadhiva/dl_prism/internal/config"
	"github.com/dhivadhiva/dl_prism/internal/provider"
	"github.com/dhivadhiva/dl_prism/internal/registry"
)

type Engine struct {
	Cfg     *config.Config
	Reg     *registry.Registry
	Breaker *Breaker
	// BudgetExceeded reports whether the monthly budget is spent; wired to
	// the cost ledger. Nil means no budget tracking.
	BudgetExceeded func() bool
}

// Decision is the routing outcome: an ordered candidate chain plus enough
// context to explain it (the spec's transparency requirement).
type Decision struct {
	Mode    string           `json:"mode"` // manual | auto
	Reason  string           `json:"reason"`
	Profile *Profile         `json:"profile,omitempty"`
	Chain   []registry.Entry `json:"-"`
}

// UnknownModelError lists what is available, so clients see actionable 404s.
type UnknownModelError struct {
	Requested string
	Available []string
}

func (e *UnknownModelError) Error() string {
	return fmt.Sprintf("unknown model %q; available: %s", e.Requested, strings.Join(e.Available, ", "))
}

// Validate resolves every rule target and fallback at startup so a bad
// config fails fast instead of at request time.
func (e *Engine) Validate() error {
	check := func(name, where string) error {
		if _, ok := e.Reg.Resolve(name); !ok {
			return fmt.Errorf("%s references unknown model %q", where, name)
		}
		return nil
	}
	if d := e.Cfg.Routing.Default; d != "" {
		if err := check(d, "routing.default"); err != nil {
			return err
		}
	}
	for i, r := range e.Cfg.Routing.Rules {
		if err := check(r.Then, fmt.Sprintf("routing rule %d", i+1)); err != nil {
			return err
		}
	}
	for from, chain := range e.Cfg.Routing.Fallbacks {
		if err := check(from, "routing.fallbacks key"); err != nil {
			return err
		}
		for _, to := range chain {
			if err := check(to, fmt.Sprintf("fallback chain of %q", from)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *Engine) state() State {
	s := State{Offline: e.Cfg.Offline}
	if e.BudgetExceeded != nil {
		s.BudgetExceeded = e.BudgetExceeded()
	}
	return s
}

func (e *Engine) Route(req *provider.ChatRequest) (*Decision, error) {
	name := strings.TrimSpace(req.Model)
	if strings.HasPrefix(strings.ToLower(name), "pipeline:") {
		return nil, fmt.Errorf("hybrid pipelines are not implemented yet (planned: M4)")
	}
	s := e.state()

	var dec *Decision
	var primary registry.Entry
	if name == "" || strings.EqualFold(name, "auto") {
		p := Classify(req)
		target, reason := Evaluate(e.Cfg.Routing.Rules, p, s)
		if target == "" {
			if e.Cfg.Routing.Default == "" {
				return nil, fmt.Errorf("no routing rule matched and routing.default is not set")
			}
			target, reason = e.Cfg.Routing.Default, "no rule matched; using routing.default"
		}
		entry, ok := e.Reg.Resolve(target)
		if !ok {
			return nil, &UnknownModelError{Requested: target, Available: e.Reg.Names()}
		}
		primary = entry
		dec = &Decision{Mode: "auto", Reason: reason, Profile: &p}
	} else {
		entry, ok := e.Reg.Resolve(name)
		if !ok {
			return nil, &UnknownModelError{Requested: name, Available: e.Reg.Names()}
		}
		primary = entry
		dec = &Decision{Mode: "manual", Reason: fmt.Sprintf("client requested %q", name)}
	}

	dec.Chain = e.buildChain(name, primary, dec.Profile, s)
	if len(dec.Chain) == 0 {
		if s.Offline {
			return nil, fmt.Errorf("offline mode: no local provider can serve this request")
		}
		return nil, fmt.Errorf("no provider can serve this request")
	}
	return dec, nil
}

// buildChain assembles primary + configured fallbacks, dropping candidates
// that cannot serve the request (offline/non-local, context too small in
// auto mode) and demoting circuit-open providers to last resort.
func (e *Engine) buildChain(requested string, primary registry.Entry, p *Profile, s State) []registry.Entry {
	candidates := []registry.Entry{primary}
	for _, key := range fallbackKeys(requested, primary) {
		if chain, ok := e.Cfg.Routing.Fallbacks[key]; ok {
			for _, name := range chain {
				if entry, ok := e.Reg.Resolve(name); ok {
					candidates = append(candidates, entry)
				}
			}
			break
		}
	}

	var healthy, demoted []registry.Entry
	seen := map[string]bool{}
	for _, c := range candidates {
		key := c.Provider.Name + "/" + c.Model.ID
		if seen[key] {
			continue
		}
		seen[key] = true
		if s.Offline && !c.Provider.Local {
			continue
		}
		// In auto mode we chose the model ourselves, so drop candidates the
		// request cannot fit into. A manual pick is the user's business.
		if p != nil && c.Model.ContextWindow > 0 && p.EstTokens > c.Model.ContextWindow {
			continue
		}
		if e.Breaker != nil && !e.Breaker.Healthy(c.Provider.Name) {
			demoted = append(demoted, c)
			continue
		}
		healthy = append(healthy, c)
	}
	return append(healthy, demoted...)
}

func fallbackKeys(requested string, primary registry.Entry) []string {
	keys := []string{}
	if requested != "" {
		keys = append(keys, requested)
	}
	keys = append(keys, primary.Model.ID)
	keys = append(keys, primary.Model.Aliases...)
	return keys
}
