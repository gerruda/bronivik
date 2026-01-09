package repository

import (
	"context"
	"testing"
	"time"

	"bronivik/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryStateRepository(t *testing.T) {
	repo := NewMemoryStateRepository(time.Hour)
	ctx := context.Background()

	t.Run("SetAndGetState", func(t *testing.T) {
		state := &models.UserState{UserID: 123, CurrentStep: "test"}
		err := repo.SetState(ctx, state)
		require.NoError(t, err)

		got, err := repo.GetState(ctx, 123)
		require.NoError(t, err)
		assert.Equal(t, state, got)
	})

	t.Run("ClearState", func(t *testing.T) {
		err := repo.ClearState(ctx, 123)
		require.NoError(t, err)
		got, _ := repo.GetState(ctx, 123)
		assert.Nil(t, got)
	})

	t.Run("RateLimit", func(t *testing.T) {
		userID := int64(456)
		allowed, _ := repo.CheckRateLimit(ctx, userID, 2, time.Second)
		assert.True(t, allowed)
		allowed, _ = repo.CheckRateLimit(ctx, userID, 2, time.Second)
		assert.True(t, allowed)
		allowed, _ = repo.CheckRateLimit(ctx, userID, 2, time.Second)
		assert.False(t, allowed)

		// Wait for expiry
		time.Sleep(time.Second + 10*time.Millisecond)
		allowed, _ = repo.CheckRateLimit(ctx, userID, 2, time.Second)
		assert.True(t, allowed)
	})
}
