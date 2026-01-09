package repository

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"bronivik/internal/models"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockRepo struct {
	mock.Mock
}

func (m *mockRepo) GetState(ctx context.Context, userID int64) (*models.UserState, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.UserState), args.Error(1)
}

func (m *mockRepo) SetState(ctx context.Context, state *models.UserState) error {
	args := m.Called(ctx, state)
	return args.Error(0)
}

func (m *mockRepo) ClearState(ctx context.Context, userID int64) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

func (m *mockRepo) CheckRateLimit(ctx context.Context, userID int64, limit int, window time.Duration) (bool, error) {
	args := m.Called(ctx, userID, limit, window)
	return args.Bool(0), args.Error(1)
}

func TestFailoverStateRepository(t *testing.T) {
	primary := new(mockRepo)
	fallback := new(mockRepo)
	logger := zerolog.New(io.Discard)
	repo := NewFailoverStateRepository(primary, fallback, &logger)
	ctx := context.Background()

	t.Run("PrimarySuccess", func(t *testing.T) {
		state := &models.UserState{UserID: 1}
		primary.On("GetState", ctx, int64(1)).Return(state, nil).Once()

		got, err := repo.GetState(ctx, 1)
		assert.NoError(t, err)
		assert.Equal(t, state, got)
		primary.AssertExpectations(t)
	})

	t.Run("PrimaryFailFallbackSuccess", func(t *testing.T) {
		state := &models.UserState{UserID: 2}
		primary.On("GetState", ctx, int64(2)).Return(nil, errors.New("fail")).Once()
		fallback.On("GetState", ctx, int64(2)).Return(state, nil).Once()

		got, err := repo.GetState(ctx, 2)
		assert.NoError(t, err)
		assert.Equal(t, state, got)
		assert.True(t, repo.isDown.Load())
		primary.AssertExpectations(t)
		fallback.AssertExpectations(t)
	})

	t.Run("RecoveryAttempt", func(t *testing.T) {
		repo.isDown.Store(true)
		repo.lastCheck = time.Now().Add(-2 * time.Minute)

		state := &models.UserState{UserID: 3}
		primary.On("GetState", ctx, int64(3)).Return(state, nil).Once()

		got, err := repo.GetState(ctx, 3)
		assert.NoError(t, err)
		assert.Equal(t, state, got)
		assert.False(t, repo.isDown.Load())
		primary.AssertExpectations(t)
	})

	t.Run("RecoveryAttemptFail", func(t *testing.T) {
		repo.isDown.Store(true)
		repo.lastCheck = time.Now().Add(-2 * time.Minute)

		primary.On("GetState", ctx, int64(33)).Return(nil, errors.New("still fail")).Once()
		fallback.On("GetState", ctx, int64(33)).Return(nil, nil).Once()

		_, err := repo.GetState(ctx, 33)
		assert.NoError(t, err)
		assert.True(t, repo.isDown.Load())
		primary.AssertExpectations(t)
		fallback.AssertExpectations(t)
	})

	t.Run("SetStateSuccess", func(t *testing.T) {
		repo.isDown.Store(false)
		state := &models.UserState{UserID: 77}
		primary.On("SetState", ctx, state).Return(nil).Once()

		err := repo.SetState(ctx, state)
		assert.NoError(t, err)
		primary.AssertExpectations(t)
	})

	t.Run("ClearStateSuccess", func(t *testing.T) {
		repo.isDown.Store(false)
		primary.On("ClearState", ctx, int64(88)).Return(nil).Once()

		err := repo.ClearState(ctx, 88)
		assert.NoError(t, err)
		primary.AssertExpectations(t)
	})

	t.Run("CheckRateLimitSuccess", func(t *testing.T) {
		repo.isDown.Store(false)
		primary.On("CheckRateLimit", ctx, int64(99), 10, time.Minute).Return(true, nil).Once()

		allowed, err := repo.CheckRateLimit(ctx, 99, 10, time.Minute)
		assert.NoError(t, err)
		assert.True(t, allowed)
		primary.AssertExpectations(t)
	})

	t.Run("SetStateFailover", func(t *testing.T) {
		repo.isDown.Store(false)
		state := &models.UserState{UserID: 4}
		primary.On("SetState", ctx, state).Return(errors.New("fail")).Once()
		fallback.On("SetState", ctx, state).Return(nil).Once()

		err := repo.SetState(ctx, state)
		assert.NoError(t, err)
		assert.True(t, repo.isDown.Load())
		primary.AssertExpectations(t)
		fallback.AssertExpectations(t)
	})

	t.Run("ClearStateFailover", func(t *testing.T) {
		repo.isDown.Store(false)
		primary.On("ClearState", ctx, int64(5)).Return(errors.New("fail")).Once()
		fallback.On("ClearState", ctx, int64(5)).Return(nil).Once()

		err := repo.ClearState(ctx, 5)
		assert.NoError(t, err)
		assert.True(t, repo.isDown.Load())
		primary.AssertExpectations(t)
		fallback.AssertExpectations(t)
	})

	t.Run("CheckRateLimitFailover", func(t *testing.T) {
		repo.isDown.Store(false)
		primary.On("CheckRateLimit", ctx, int64(6), 10, time.Minute).Return(false, errors.New("fail")).Once()
		fallback.On("CheckRateLimit", ctx, int64(6), 10, time.Minute).Return(true, nil).Once()

		allowed, err := repo.CheckRateLimit(ctx, 6, 10, time.Minute)
		assert.NoError(t, err)
		assert.True(t, allowed)
		assert.True(t, repo.isDown.Load())
		primary.AssertExpectations(t)
		fallback.AssertExpectations(t)
	})

	t.Run("SetStateAlreadyDown", func(t *testing.T) {
		repo.isDown.Store(true)
		state := &models.UserState{UserID: 44}
		fallback.On("SetState", ctx, state).Return(nil).Once()

		err := repo.SetState(ctx, state)
		assert.NoError(t, err)
		fallback.AssertExpectations(t)
	})

	t.Run("ClearStateAlreadyDown", func(t *testing.T) {
		repo.isDown.Store(true)
		fallback.On("ClearState", ctx, int64(55)).Return(nil).Once()

		err := repo.ClearState(ctx, 55)
		assert.NoError(t, err)
		fallback.AssertExpectations(t)
	})

	t.Run("CheckRateLimitAlreadyDown", func(t *testing.T) {
		repo.isDown.Store(true)
		fallback.On("CheckRateLimit", ctx, int64(66), 10, time.Minute).Return(true, nil).Once()

		allowed, err := repo.CheckRateLimit(ctx, 66, 10, time.Minute)
		assert.NoError(t, err)
		assert.True(t, allowed)
		fallback.AssertExpectations(t)
	})

	t.Run("HealthCheckDoesNothing", func(t *testing.T) {
		repo.checkHealth() // Just for coverage
	})
}
