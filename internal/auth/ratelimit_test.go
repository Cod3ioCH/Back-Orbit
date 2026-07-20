package auth

import (
	"testing"
	"time"
)

func TestLoginRateLimiterAllowsUpToMaxAttempts(t *testing.T) {
	limiter := NewLoginRateLimiter(3, time.Minute, time.Minute)
	key := "127.0.0.1:admin"

	// maxAttempts=3 means 3 failed attempts are tolerated before blocking.
	for i := 0; i < 3; i++ {
		if !limiter.Allow(key) {
			t.Fatalf("expected attempt %d to be allowed", i+1)
		}
		limiter.RecordFailure(key)
	}
}

func TestLoginRateLimiterBlocksAfterExceedingMaxAttempts(t *testing.T) {
	limiter := NewLoginRateLimiter(3, time.Minute, time.Minute)
	key := "127.0.0.1:admin"

	for i := 0; i < 4; i++ {
		limiter.RecordFailure(key)
	}

	if limiter.Allow(key) {
		t.Fatal("expected limiter to block after exceeding max attempts")
	}
}

func TestLoginRateLimiterResetsOnSuccess(t *testing.T) {
	limiter := NewLoginRateLimiter(3, time.Minute, time.Minute)
	key := "127.0.0.1:admin"

	limiter.RecordFailure(key)
	limiter.RecordFailure(key)
	limiter.RecordSuccess(key)

	if !limiter.Allow(key) {
		t.Fatal("expected limiter to allow attempts again after a recorded success")
	}
}

func TestLoginRateLimiterIsolatesKeys(t *testing.T) {
	limiter := NewLoginRateLimiter(1, time.Minute, time.Minute)

	limiter.RecordFailure("127.0.0.1:alice")
	limiter.RecordFailure("127.0.0.1:alice")

	if limiter.Allow("127.0.0.1:alice") {
		t.Fatal("expected alice to be rate-limited")
	}
	if !limiter.Allow("127.0.0.1:bob") {
		t.Fatal("expected a different key to be unaffected")
	}
}

func TestLoginRateLimiterBackoffExpires(t *testing.T) {
	limiter := NewLoginRateLimiter(1, 20*time.Millisecond, 20*time.Millisecond)
	key := "127.0.0.1:admin"

	limiter.RecordFailure(key)
	limiter.RecordFailure(key)
	if limiter.Allow(key) {
		t.Fatal("expected limiter to block immediately after exceeding max attempts")
	}

	time.Sleep(40 * time.Millisecond)

	if !limiter.Allow(key) {
		t.Fatal("expected limiter to allow attempts again after backoff expires")
	}
}
