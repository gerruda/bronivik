package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"bronivik/internal/models"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockStateRepository struct {
	mock.Mock
}

func (m *MockStateRepository) GetState(ctx context.Context, userID int64) (*models.UserState, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.UserState), args.Error(1)
}

func (m *MockStateRepository) SetState(ctx context.Context, state *models.UserState) error {
	args := m.Called(ctx, state)
	return args.Error(0)
}

func (m *MockStateRepository) ClearState(ctx context.Context, userID int64) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

func (m *MockStateRepository) CheckRateLimit(ctx context.Context, userID int64, limit int, window time.Duration) (bool, error) {
	args := m.Called(ctx, userID, limit, window)
	return args.Bool(0), args.Error(1)
}

func TestStateService_GetUserState(t *testing.T) {
	mockRepo := new(MockStateRepository)
	logger := zerolog.Nop()
	s := NewStateService(mockRepo, &logger)
	ctx := context.Background()
	userID := int64(123)

	t.Run("Success", func(t *testing.T) {
		expectedState := &models.UserState{UserID: userID, CurrentStep: "test"}
		mockRepo.On("GetState", ctx, userID).Return(expectedState, nil).Once()

		state, err := s.GetUserState(ctx, userID)
		assert.NoError(t, err)
		assert.Equal(t, expectedState, state)
	})

	t.Run("Error", func(t *testing.T) {
		mockRepo.On("GetState", ctx, userID).Return(nil, errors.New("db error")).Once()

		state, err := s.GetUserState(ctx, userID)
		assert.Error(t, err)
		assert.Nil(t, state)
	})
}

func TestStateService_SetUserState(t *testing.T) {
	mockRepo := new(MockStateRepository)
	logger := zerolog.Nop()
	s := NewStateService(mockRepo, &logger)
	ctx := context.Background()
	userID := int64(123)

	t.Run("Success", func(t *testing.T) {
		mockRepo.On("SetState", ctx, mock.MatchedBy(func(state *models.UserState) bool {
			return state.UserID == userID && state.CurrentStep == "step1"
		})).Return(nil).Once()

		err := s.SetUserState(ctx, userID, "step1", nil)
		assert.NoError(t, err)
	})
}

func TestStateService_UpdateUserStateData(t *testing.T) {
	mockRepo := new(MockStateRepository)
	logger := zerolog.Nop()
	s := NewStateService(mockRepo, &logger)
	ctx := context.Background()
	userID := int64(123)

	t.Run("UpdateExisting", func(t *testing.T) {
		initialState := &models.UserState{
			UserID:   userID,
			TempData: map[string]interface{}{"old": "val"},
		}
		mockRepo.On("GetState", ctx, userID).Return(initialState, nil).Once()
		mockRepo.On("SetState", ctx, mock.MatchedBy(func(state *models.UserState) bool {
			return state.TempData["new"] == "val2" && state.TempData["old"] == "val"
		})).Return(nil).Once()

		err := s.UpdateUserStateData(ctx, userID, "new", "val2")
		assert.NoError(t, err)
	})

	t.Run("CreateNew", func(t *testing.T) {
		mockRepo.On("GetState", ctx, userID).Return(nil, nil).Once()
		mockRepo.On("SetState", ctx, mock.MatchedBy(func(state *models.UserState) bool {
			return state.TempData["key"] == "value"
		})).Return(nil).Once()

		err := s.UpdateUserStateData(ctx, userID, "key", "value")
		assert.NoError(t, err)
	})
}

func TestStateService_CheckRateLimit(t *testing.T) {
	mockRepo := new(MockStateRepository)
	logger := zerolog.Nop()
	s := NewStateService(mockRepo, &logger)
	ctx := context.Background()
	userID := int64(123)

	t.Run("Allowed", func(t *testing.T) {
		mockRepo.On("CheckRateLimit", ctx, userID, 5, time.Minute).Return(true, nil).Once()
		allowed, err := s.CheckRateLimit(ctx, userID, 5, time.Minute)
		assert.NoError(t, err)
		assert.True(t, allowed)
	})
}
