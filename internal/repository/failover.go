package repository

import (
	"context"
	"sync/atomic"
	"time"

	"bronivik/internal/domain"
	"bronivik/internal/models"

	"github.com/rs/zerolog"
)

type FailoverStateRepository struct {
	primary   domain.StateRepository
	fallback  domain.StateRepository
	logger    *zerolog.Logger
	isDown    atomic.Bool
	lastCheck time.Time
}

func NewFailoverStateRepository(primary, fallback domain.StateRepository, logger *zerolog.Logger) *FailoverStateRepository {
	return &FailoverStateRepository{
		primary:  primary,
		fallback: fallback,
		logger:   logger,
	}
}

func (r *FailoverStateRepository) checkHealth() {
	// Simple health check logic could be added here if needed
	// For now, we rely on error detection during calls
}

func (r *FailoverStateRepository) GetState(ctx context.Context, userID int64) (*models.UserState, error) {
	if !r.isDown.Load() {
		state, err := r.primary.GetState(ctx, userID)
		if err == nil {
			return state, nil
		}
		r.logger.Error().Err(err).Msg("Primary state repository failed, falling back to memory")
		r.isDown.Store(true)
		r.lastCheck = time.Now()
	}

	// Try to recover after 1 minute
	if r.isDown.Load() && time.Since(r.lastCheck) > time.Minute {
		state, err := r.primary.GetState(ctx, userID)
		if err == nil {
			r.isDown.Store(false)
			return state, nil
		}
		r.lastCheck = time.Now()
	}

	return r.fallback.GetState(ctx, userID)
}

func (r *FailoverStateRepository) SetState(ctx context.Context, state *models.UserState) error {
	if !r.isDown.Load() {
		err := r.primary.SetState(ctx, state)
		if err == nil {
			return nil
		}
		r.logger.Error().Err(err).Msg("Primary state repository failed, falling back to memory")
		r.isDown.Store(true)
		r.lastCheck = time.Now()
	}

	return r.fallback.SetState(ctx, state)
}

func (r *FailoverStateRepository) ClearState(ctx context.Context, userID int64) error {
	if !r.isDown.Load() {
		err := r.primary.ClearState(ctx, userID)
		if err == nil {
			return nil
		}
		r.logger.Error().Err(err).Msg("Primary state repository failed, falling back to memory")
		r.isDown.Store(true)
		r.lastCheck = time.Now()
	}

	return r.fallback.ClearState(ctx, userID)
}

func (r *FailoverStateRepository) CheckRateLimit(ctx context.Context, userID int64, limit int, window time.Duration) (bool, error) {
	if !r.isDown.Load() {
		allowed, err := r.primary.CheckRateLimit(ctx, userID, limit, window)
		if err == nil {
			return allowed, nil
		}
		r.logger.Error().Err(err).Msg("Primary state repository failed, falling back to memory")
		r.isDown.Store(true)
		r.lastCheck = time.Now()
	}

	return r.fallback.CheckRateLimit(ctx, userID, limit, window)
}
