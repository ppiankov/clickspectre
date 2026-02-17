package collector

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

const (
	maxRetryAttempts    = 3
	initialRetryBackoff = 100 * time.Millisecond
	maxRetryBackoff     = 2 * time.Second
)

var (
	authErrorSubstrings = []string{
		"authentication failed",
		"authentication error",
		"invalid credentials",
		"invalid password",
		"password is incorrect",
		"wrong password",
		"unknown user",
		"unauthorized",
		"access denied",
		"sqlstate[28000]",
		"sqlstate 28000",
		"code: 193",
		"code: 194",
		"code: 497",
		"code: 516",
	}
	retryableErrorSubstrings = []string{
		"timeout",
		"i/o timeout",
		"tls handshake timeout",
		"eof",
		"unexpected eof",
		"broken pipe",
		"connection reset",
		"connection refused",
		"connection aborted",
		"connection closed",
		"use of closed network connection",
		"network is unreachable",
		"no route to host",
		"no such host",
	}
)

type retryConfig struct {
	maxAttempts    int
	initialBackoff time.Duration
	maxBackoff     time.Duration
	sleep          func(context.Context, time.Duration) error
}

func defaultRetryConfig() retryConfig {
	return retryConfig{
		maxAttempts:    maxRetryAttempts,
		initialBackoff: initialRetryBackoff,
		maxBackoff:     maxRetryBackoff,
		sleep:          sleepWithContext,
	}
}

func (cfg retryConfig) normalized() retryConfig {
	if cfg.maxAttempts <= 0 {
		cfg.maxAttempts = maxRetryAttempts
	}
	if cfg.initialBackoff <= 0 {
		cfg.initialBackoff = initialRetryBackoff
	}
	if cfg.maxBackoff <= 0 {
		cfg.maxBackoff = maxRetryBackoff
	}
	if cfg.sleep == nil {
		cfg.sleep = sleepWithContext
	}
	if cfg.maxBackoff < cfg.initialBackoff {
		cfg.maxBackoff = cfg.initialBackoff
	}
	return cfg
}

func executeWithRetry(ctx context.Context, cfg retryConfig, fn func() error) error {
	cfg = cfg.normalized()
	backoff := cfg.initialBackoff

	var lastErr error
	for attempt := 1; attempt <= cfg.maxAttempts; attempt++ {
		if err := contextError(ctx); err != nil {
			return err
		}

		err := fn()
		if err == nil {
			return nil
		}
		lastErr = err
		if ctxErr := contextError(ctx); ctxErr != nil {
			return ctxErr
		}

		if isAuthError(err) || !isRetryableError(err) || attempt == cfg.maxAttempts {
			return err
		}

		if err := cfg.sleep(ctx, backoff); err != nil {
			if ctxErr := contextError(ctx); ctxErr != nil {
				return ctxErr
			}
			return err
		}

		if backoff < cfg.maxBackoff {
			backoff *= 2
			if backoff > cfg.maxBackoff {
				backoff = cfg.maxBackoff
			}
		}
	}

	return lastErr
}

func withTotalTimeoutContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return parent, func() {}
	}

	ctx, cancelCause := context.WithCancelCause(parent)
	timer := time.AfterFunc(timeout, func() {
		cancelCause(context.DeadlineExceeded)
	})

	return ctx, func() {
		timer.Stop()
		cancelCause(context.Canceled)
	}
}

func contextError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) {
			return cause
		}
		return err
	}
	return nil
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return contextError(ctx)
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isAuthError(err error) bool {
	if err == nil {
		return false
	}

	var chErr *clickhouse.Exception
	if errors.As(err, &chErr) {
		switch chErr.Code {
		case 193, 194, 497, 516:
			return true
		}
	}

	errText := strings.ToLower(err.Error())
	for _, marker := range authErrorSubstrings {
		if strings.Contains(errText, marker) {
			return true
		}
	}

	return false
}

func isRetryableError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}

	errText := strings.ToLower(err.Error())
	for _, marker := range retryableErrorSubstrings {
		if strings.Contains(errText, marker) {
			return true
		}
	}

	return false
}
