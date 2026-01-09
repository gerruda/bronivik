package api

import (
	"sync"

	"bronivik/internal/config"

	"golang.org/x/time/rate"
)

type rateLimiter struct {
	limiters sync.Map
	cfg      *config.APIConfig
}

func newRateLimiter(cfg *config.APIConfig) *rateLimiter {
	return &rateLimiter{
		cfg: cfg,
	}
}

func (l *rateLimiter) getLimiter(key string) *rate.Limiter {
	if v, ok := l.limiters.Load(key); ok {
		if lim, ok := v.(*rate.Limiter); ok {
			return lim
		}
	}

	burst := l.cfg.RateLimit.Burst
	if burst <= 0 {
		burst = 5
	}

	lim := rate.NewLimiter(rate.Limit(l.cfg.RateLimit.RPS), burst)
	actual, loaded := l.limiters.LoadOrStore(key, lim)
	if loaded {
		if actualLim, ok := actual.(*rate.Limiter); ok {
			return actualLim
		}
	}
	return lim
}
