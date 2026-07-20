package auth

import (
	"sync"
	"time"
)

// LoginRateLimiter throttles login attempts per key (typically IP+username)
// with exponential backoff after repeated failures. It is intentionally
// in-memory and per-process: Back-Orbit runs as a single process (ADR-0001),
// so this needs no shared/distributed state.
type LoginRateLimiter struct {
	mu          sync.Mutex
	records     map[string]*loginAttemptRecord
	maxAttempts int
	window      time.Duration
	maxBackoff  time.Duration
}

type loginAttemptRecord struct {
	failures     int
	windowStart  time.Time
	blockedUntil time.Time
}

// NewLoginRateLimiter creates a limiter that allows maxAttempts failures per
// window before blocking, with exponential backoff up to maxBackoff.
func NewLoginRateLimiter(maxAttempts int, window, maxBackoff time.Duration) *LoginRateLimiter {
	return &LoginRateLimiter{
		records:     make(map[string]*loginAttemptRecord),
		maxAttempts: maxAttempts,
		window:      window,
		maxBackoff:  maxBackoff,
	}
}

// Allow reports whether an attempt for key is currently permitted.
func (l *LoginRateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	rec, ok := l.records[key]
	if !ok {
		return true
	}

	now := time.Now()
	if now.Before(rec.blockedUntil) {
		return false
	}
	if now.Sub(rec.windowStart) > l.window && rec.blockedUntil.IsZero() {
		delete(l.records, key)
		return true
	}
	return true
}

// RecordFailure registers a failed attempt for key, extending the block
// duration exponentially once maxAttempts is exceeded within the window.
func (l *LoginRateLimiter) RecordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	rec, ok := l.records[key]
	if !ok || now.Sub(rec.windowStart) > l.window {
		rec = &loginAttemptRecord{windowStart: now}
		l.records[key] = rec
	}

	rec.failures++
	if rec.failures > l.maxAttempts {
		backoff := l.window << (rec.failures - l.maxAttempts - 1)
		if backoff > l.maxBackoff || backoff <= 0 {
			backoff = l.maxBackoff
		}
		rec.blockedUntil = now.Add(backoff)
	}
}

// RecordSuccess clears any failure history for key.
func (l *LoginRateLimiter) RecordSuccess(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.records, key)
}
