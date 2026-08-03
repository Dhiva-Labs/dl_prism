package router

import (
	"testing"
	"time"
)

func TestBreakerHealthyByDefault(t *testing.T) {
	b := NewBreaker()
	if !b.Healthy("p") {
		t.Error("Healthy() = false, want true for a provider with no recorded failures")
	}
}

func TestBreakerOpensAfterThreeFailures(t *testing.T) {
	b := NewBreaker()
	b.Failure("p")
	b.Failure("p")
	if !b.Healthy("p") {
		t.Error("Healthy() = false after 2 failures, want true (threshold is 3)")
	}
	b.Failure("p")
	if b.Healthy("p") {
		t.Error("Healthy() = true after 3 failures, want false (circuit should be open)")
	}
}

func TestBreakerSuccessResets(t *testing.T) {
	b := NewBreaker()
	b.Failure("p")
	b.Failure("p")
	b.Failure("p")
	if b.Healthy("p") {
		t.Fatal("Healthy() = true after 3 failures, want false")
	}
	b.Success("p")
	if !b.Healthy("p") {
		t.Error("Healthy() = false after Success(), want true (should reset)")
	}
}

func TestBreakerClosesAfterCooldownExpires(t *testing.T) {
	b := NewBreaker()
	cur := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	b.now = func() time.Time { return cur }

	b.Failure("p")
	b.Failure("p")
	b.Failure("p")
	if b.Healthy("p") {
		t.Fatal("Healthy() = true immediately after opening, want false")
	}

	cur = cur.Add(breakerCooldown - time.Second)
	if b.Healthy("p") {
		t.Error("Healthy() = true before cooldown elapsed, want false")
	}

	cur = cur.Add(2 * time.Second) // now past breakerCooldown since it opened
	if !b.Healthy("p") {
		t.Error("Healthy() = false after cooldown elapsed, want true (circuit should close)")
	}
}
