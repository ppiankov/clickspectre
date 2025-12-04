package k8s

import (
	"context"

	"golang.org/x/time/rate"
)

// RateLimiter provides rate limiting for Kubernetes API calls
type RateLimiter struct {
	limiter *rate.Limiter
}

// NewRateLimiter creates a new rate limiter
// rps: requests per second
func NewRateLimiter(rps int) *RateLimiter {
	// Create token bucket limiter
	// rps: rate of token replenishment
	// burst: maximum burst size (2x the rate)
	return &RateLimiter{
		limiter: rate.NewLimiter(rate.Limit(rps), rps*2),
	}
}

// Wait blocks until the rate limiter allows an action
func (r *RateLimiter) Wait(ctx context.Context) error {
	return r.limiter.Wait(ctx)
}

// Allow checks if an action is allowed without blocking
func (r *RateLimiter) Allow() bool {
	return r.limiter.Allow()
}
