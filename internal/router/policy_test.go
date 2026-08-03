package router

import (
	"strings"
	"testing"

	"github.com/dhivadhiva/dl_prism/internal/config"
)

func boolPtr(b bool) *bool { return &b }

func TestEvaluateFirstMatchWins(t *testing.T) {
	rules := []config.Rule{
		{If: config.Condition{Task: "code"}, Then: "first"},
		{If: config.Condition{Task: "code"}, Then: "second"},
	}
	target, reason := Evaluate(rules, Profile{Task: "code"}, State{})
	if target != "first" {
		t.Errorf("target = %q, want %q", target, "first")
	}
	if !strings.Contains(reason, "rule 1 matched") {
		t.Errorf("reason = %q, want it to mention rule 1", reason)
	}
}

func TestEvaluateUnsetFieldsMatchAnything(t *testing.T) {
	rules := []config.Rule{
		{If: config.Condition{Task: "code"}, Then: "target"},
	}
	target, _ := Evaluate(rules, Profile{Task: "code", Language: "rust", Vision: false}, State{Offline: true, BudgetExceeded: true})
	if target != "target" {
		t.Errorf("target = %q, want match despite unrelated unset condition fields", target)
	}
}

func TestEvaluateEmptyConditionIsCatchAll(t *testing.T) {
	rules := []config.Rule{
		{If: config.Condition{}, Then: "catchall"},
	}
	target, reason := Evaluate(rules, Profile{Task: "anything", Language: "whatever"}, State{Offline: true})
	if target != "catchall" {
		t.Errorf("target = %q, want %q", target, "catchall")
	}
	if !strings.Contains(reason, "always") {
		t.Errorf("reason = %q, want it to describe an empty condition as always", reason)
	}
}

func TestEvaluateNoMatchReturnsEmpty(t *testing.T) {
	rules := []config.Rule{
		{If: config.Condition{Task: "vision"}, Then: "x"},
	}
	target, reason := Evaluate(rules, Profile{Task: "code"}, State{})
	if target != "" || reason != "" {
		t.Errorf("got target=%q reason=%q, want both empty when nothing matches", target, reason)
	}
}

func TestMatchesConditionFields(t *testing.T) {
	tests := []struct {
		name string
		cond config.Condition
		p    Profile
		s    State
		want bool
	}{
		{"task match", config.Condition{Task: "code"}, Profile{Task: "code"}, State{}, true},
		{"task mismatch", config.Condition{Task: "code"}, Profile{Task: "docs"}, State{}, false},
		{"language case-insensitive match", config.Condition{Language: "Python"}, Profile{Language: "python"}, State{}, true},
		{"language mismatch", config.Condition{Language: "python"}, Profile{Language: "go"}, State{}, false},
		{"vision true match", config.Condition{Vision: boolPtr(true)}, Profile{Vision: true}, State{}, true},
		{"vision mismatch", config.Condition{Vision: boolPtr(true)}, Profile{Vision: false}, State{}, false},
		{"offline true match", config.Condition{Offline: boolPtr(true)}, Profile{}, State{Offline: true}, true},
		{"offline mismatch", config.Condition{Offline: boolPtr(true)}, Profile{}, State{Offline: false}, false},
		{"budget_exceeded match", config.Condition{BudgetExceeded: boolPtr(true)}, Profile{}, State{BudgetExceeded: true}, true},
		{"budget_exceeded mismatch", config.Condition{BudgetExceeded: boolPtr(false)}, Profile{}, State{BudgetExceeded: true}, false},
		{"min_context_tokens satisfied", config.Condition{MinContextTokens: 100}, Profile{EstTokens: 150}, State{}, true},
		{"min_context_tokens not satisfied", config.Condition{MinContextTokens: 100}, Profile{EstTokens: 50}, State{}, false},
		{"empty condition always matches", config.Condition{}, Profile{Task: "x", EstTokens: 0}, State{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matches(tt.cond, tt.p, tt.s)
			if got != tt.want {
				t.Errorf("matches() = %v, want %v", got, tt.want)
			}
		})
	}
}
