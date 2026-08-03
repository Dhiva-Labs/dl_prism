package router

import (
	"fmt"
	"strings"

	"github.com/dhivadhiva/dl_prism/internal/config"
)

// State holds request-independent facts the policy engine can match on.
type State struct {
	Offline        bool
	BudgetExceeded bool
}

// Evaluate walks the ordered rules and returns the first full match's
// target plus a human-readable reason. Empty target means nothing matched.
func Evaluate(rules []config.Rule, p Profile, s State) (target, reason string) {
	for i, r := range rules {
		if matches(r.If, p, s) {
			return r.Then, fmt.Sprintf("rule %d matched (%s)", i+1, r.If.Describe())
		}
	}
	return "", ""
}

func matches(c config.Condition, p Profile, s State) bool {
	if c.Task != "" && !strings.EqualFold(c.Task, p.Task) {
		return false
	}
	if c.Language != "" && !strings.EqualFold(c.Language, p.Language) {
		return false
	}
	if c.Vision != nil && *c.Vision != p.Vision {
		return false
	}
	if c.Offline != nil && *c.Offline != s.Offline {
		return false
	}
	if c.BudgetExceeded != nil && *c.BudgetExceeded != s.BudgetExceeded {
		return false
	}
	if c.MinContextTokens > 0 && p.EstTokens < c.MinContextTokens {
		return false
	}
	return true
}
