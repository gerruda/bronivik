package worker

import (
	"math"
	"time"
)

// RetryPolicy defines exponential backoff parameters.
type RetryPolicy struct {
	MaxRetries    int
	InitialDelay  time.Duration
	MaxDelay      time.Duration
	BackoffFactor float64
}

// NextDelay returns delay for a given attempt (1-based) with clamping.
func (r RetryPolicy) NextDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if r.InitialDelay <= 0 {
		r.InitialDelay = time.Second
	}
	if r.BackoffFactor <= 0 {
		r.BackoffFactor = 2
	}

	delay := float64(r.InitialDelay) * math.Pow(r.BackoffFactor, float64(attempt-1))
	d := time.Duration(delay)
	if r.MaxDelay > 0 && d > r.MaxDelay {
		d = r.MaxDelay
	}
	if d <= 0 {
		d = time.Second
	}
	return d
}
