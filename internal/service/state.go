package service

import (
	"context"
	"time"

	"bronivik/internal/domain"
	"bronivik/internal/models"

	"github.com/rs/zerolog"
)

type StateService struct {
	stateRepo domain.StateRepository
	logger    *zerolog.Logger
}

func NewStateService(stateRepo domain.StateRepository, logger *zerolog.Logger) *StateService {
	return &StateService{
		stateRepo: stateRepo,
		logger:    logger,
	}
}

func (s *StateService) GetUserState(ctx context.Context, userID int64) (*models.UserState, error) {
	state, err := s.stateRepo.GetState(ctx, userID)
	if err != nil {
		s.logger.Error().Err(err).Int64("user_id", userID).Msg("failed to get user state")
		return nil, err
	}

	return state, nil
}

func (s *StateService) SetUserState(ctx context.Context, userID int64, step string, data map[string]interface{}) error {
	state := &models.UserState{
		UserID:      userID,
		CurrentStep: step,
		TempData:    data,
	}
	return s.stateRepo.SetState(ctx, state)
}

func (s *StateService) ClearUserState(ctx context.Context, userID int64) error {
	return s.stateRepo.ClearState(ctx, userID)
}

func (s *StateService) UpdateUserStateData(ctx context.Context, userID int64, key string, value interface{}) error {
	state, err := s.stateRepo.GetState(ctx, userID)
	if err != nil {
		return err
	}
	if state == nil {
		state = &models.UserState{
			UserID:   userID,
			TempData: make(map[string]interface{}),
		}
	}

	if state.TempData == nil {
		state.TempData = make(map[string]interface{})
	}
	state.TempData[key] = value

	return s.stateRepo.SetState(ctx, state)
}

func (s *StateService) CheckRateLimit(ctx context.Context, userID int64, limit int, window time.Duration) (bool, error) {
	return s.stateRepo.CheckRateLimit(ctx, userID, limit, window)
}
