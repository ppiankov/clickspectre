package collector

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestExecuteWithRetryTransientBackoff(t *testing.T) {
	attempts := 0
	var sleeps []time.Duration

	cfg := retryConfig{
		maxAttempts:    3,
		initialBackoff: 10 * time.Millisecond,
		maxBackoff:     40 * time.Millisecond,
		sleep: func(_ context.Context, d time.Duration) error {
			sleeps = append(sleeps, d)
			return nil
		},
	}

	err := executeWithRetry(context.Background(), cfg, func() error {
		attempts++
		if attempts < 3 {
			return errors.New("i/o timeout")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected retry success, got %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
	if len(sleeps) != 2 {
		t.Fatalf("expected 2 backoff sleeps, got %d", len(sleeps))
	}
	if sleeps[0] != 10*time.Millisecond || sleeps[1] != 20*time.Millisecond {
		t.Fatalf("unexpected backoff schedule: %v", sleeps)
	}
}

func TestExecuteWithRetryAuthFailFast(t *testing.T) {
	attempts := 0
	sleepCalls := 0

	cfg := retryConfig{
		maxAttempts:    3,
		initialBackoff: 10 * time.Millisecond,
		maxBackoff:     40 * time.Millisecond,
		sleep: func(context.Context, time.Duration) error {
			sleepCalls++
			return nil
		},
	}

	err := executeWithRetry(context.Background(), cfg, func() error {
		attempts++
		return errors.New("code: 516, message: Authentication failed")
	})
	if err == nil {
		t.Fatal("expected auth error")
	}
	if attempts != 1 {
		t.Fatalf("expected auth fail-fast after 1 attempt, got %d", attempts)
	}
	if sleepCalls != 0 {
		t.Fatalf("expected no backoff sleeps for auth errors, got %d", sleepCalls)
	}
}

func TestWithTotalTimeoutContextDeadlineCause(t *testing.T) {
	ctx, cancel := withTotalTimeoutContext(context.Background(), 20*time.Millisecond)
	defer cancel()

	select {
	case <-ctx.Done():
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected timeout context to finish")
	}

	if !errors.Is(context.Cause(ctx), context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded cause, got %v", context.Cause(ctx))
	}
}
