package repository

import (
	"context"
	"testing"
	"time"

	"bronivik/internal/config"
	"bronivik/internal/models"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisStateRepository(t *testing.T) {
	s, err := miniredis.Run()
	require.NoError(t, err)
	defer s.Close()

	client := redis.NewClient(&redis.Options{
		Addr: s.Addr(),
	})
	defer client.Close()

	repo := NewRedisStateRepository(client, time.Hour)
	ctx := context.Background()

	t.Run("SetAndGetState", func(t *testing.T) {
		state := &models.UserState{
			UserID:      123,
			CurrentStep: "awaiting_name",
			TempData:    map[string]interface{}{"key": "value"},
		}

		err := repo.SetState(ctx, state)
		require.NoError(t, err)

		got, err := repo.GetState(ctx, 123)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, state.UserID, got.UserID)
		assert.Equal(t, state.CurrentStep, got.CurrentStep)
		assert.Equal(t, state.TempData["key"], got.TempData["key"])
	})

	t.Run("GetNonExistentState", func(t *testing.T) {
		got, err := repo.GetState(ctx, 999)
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("ClearState", func(t *testing.T) {
		state := &models.UserState{UserID: 456, CurrentStep: "test"}
		err := repo.SetState(ctx, state)
		require.NoError(t, err)

		err = repo.ClearState(ctx, 456)
		require.NoError(t, err)

		got, _ := repo.GetState(ctx, 456)
		assert.Nil(t, got)
	})

	t.Run("RateLimit", func(t *testing.T) {
		userID := int64(789)
		limit := 2
		window := time.Second

		// First request
		allowed, err := repo.CheckRateLimit(ctx, userID, limit, window)
		require.NoError(t, err)
		assert.True(t, allowed)

		// Second request
		allowed, err = repo.CheckRateLimit(ctx, userID, limit, window)
		require.NoError(t, err)
		assert.True(t, allowed)

		// Third request (exceeds limit)
		allowed, err = repo.CheckRateLimit(ctx, userID, limit, window)
		require.NoError(t, err)
		assert.False(t, allowed)

		// Wait for window to expire
		s.FastForward(window + time.Millisecond)

		// Should be allowed again
		allowed, err = repo.CheckRateLimit(ctx, userID, limit, window)
		require.NoError(t, err)
		assert.True(t, allowed)
	})

	t.Run("NilClient", func(t *testing.T) {
		nilRepo := NewRedisStateRepository(nil, time.Hour)
		_, err := nilRepo.GetState(ctx, 123)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "redis client is nil")
	})

	t.Run("Ping", func(t *testing.T) {
		err := Ping(ctx, client)
		assert.NoError(t, err)
	})

	t.Run("UnmarshalError", func(t *testing.T) {
		key := "user_state:999"
		err := s.Set(key, "invalid json")
		require.NoError(t, err)
		_, err = repo.GetState(ctx, 999)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal state")
	})

	t.Run("MiscCoverage", func(t *testing.T) {
		err := Close(nil)
		assert.NoError(t, err)

		repo.client = nil
		err = repo.SetState(ctx, &models.UserState{UserID: 1})
		assert.Error(t, err)
		err = repo.ClearState(ctx, 1)
		assert.Error(t, err)
		_, err = repo.CheckRateLimit(ctx, 1, 1, time.Second)
		assert.Error(t, err)

		// Test marshal error
		repo.client = client
		err = repo.SetState(ctx, &models.UserState{
			TempData: map[string]interface{}{
				"fn": func() {},
			},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to marshal state")
	})

	t.Run("NewRedisClient", func(t *testing.T) {
		cfg := config.RedisConfig{
			Address: "localhost:6379",
		}
		c := NewRedisClient(cfg)
		assert.NotNil(t, c)
		c.Close()
	})

	t.Run("RedisErrors", func(t *testing.T) {
		// Create a client and close it to trigger errors
		badClient := redis.NewClient(&redis.Options{Addr: "localhost:0"})
		badClient.Close()
		badRepo := NewRedisStateRepository(badClient, time.Hour)

		_, err := badRepo.GetState(ctx, 123)
		assert.Error(t, err)

		err = badRepo.SetState(ctx, &models.UserState{UserID: 123})
		assert.Error(t, err)

		err = badRepo.ClearState(ctx, 123)
		assert.Error(t, err)

		_, err = badRepo.CheckRateLimit(ctx, 123, 10, time.Minute)
		assert.Error(t, err)

		err = Ping(ctx, badClient)
		assert.Error(t, err)
	})

	t.Run("Close", func(t *testing.T) {
		err := Close(client)
		assert.NoError(t, err)
	})
}
