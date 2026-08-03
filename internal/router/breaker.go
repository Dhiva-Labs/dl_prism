package router

import (
	"sync"
	"time"
)

const (
	breakerThreshold = 3
	breakerCooldown  = 30 * time.Second
)

// Breaker is a per-provider circuit breaker: breakerThreshold consecutive
// failures open the circuit for breakerCooldown, demoting the provider to
// last resort in failover chains.
type Breaker struct {
	mu        sync.Mutex
	fails     map[string]int
	openUntil map[string]time.Time
	now       func() time.Time
}

func NewBreaker() *Breaker {
	return &Breaker{fails: map[string]int{}, openUntil: map[string]time.Time{}, now: time.Now}
}

func (b *Breaker) Failure(name string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fails[name]++
	if b.fails[name] >= breakerThreshold {
		b.openUntil[name] = b.now().Add(breakerCooldown)
	}
}

func (b *Breaker) Success(name string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.fails, name)
	delete(b.openUntil, name)
}

func (b *Breaker) Healthy(name string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	until, open := b.openUntil[name]
	if !open {
		return true
	}
	if b.now().After(until) {
		delete(b.openUntil, name)
		b.fails[name] = 0
		return true
	}
	return false
}
