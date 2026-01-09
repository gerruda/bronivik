package repository

import (
	"context"
	"sync"
	"time"

	"bronivik/internal/models"
)

type MemoryStateRepository struct {
	states     sync.Map
	rateLimits sync.Map
	ttl        time.Duration
}

func NewMemoryStateRepository(ttl time.Duration) *MemoryStateRepository {
	return &MemoryStateRepository{
		ttl: ttl,
	}
}

func (r *MemoryStateRepository) GetState(ctx context.Context, userID int64) (*models.UserState, error) {
	val, ok := r.states.Load(userID)
	if !ok {
		return nil, nil
	}
	return val.(*models.UserState), nil
}

func (r *MemoryStateRepository) SetState(ctx context.Context, state *models.UserState) error {
	r.states.Store(state.UserID, state)
	return nil
}

func (r *MemoryStateRepository) ClearState(ctx context.Context, userID int64) error {
	r.states.Delete(userID)
	return nil
}

type rateLimitEntry struct {
	count     int
	expiresAt time.Time
}

func (r *MemoryStateRepository) CheckRateLimit(ctx context.Context, userID int64, limit int, window time.Duration) (bool, error) {
	now := time.Now()
	val, ok := r.rateLimits.Load(userID)

	var entry *rateLimitEntry
	if !ok {
		entry = &rateLimitEntry{
			count:     1,
			expiresAt: now.Add(window),
		}
	} else {
		entry = val.(*rateLimitEntry)
		if now.After(entry.expiresAt) {
			entry.count = 1
			entry.expiresAt = now.Add(window)
		} else {
			entry.count++
		}
	}

	r.rateLimits.Store(userID, entry)
	return entry.count <= limit, nil
}
