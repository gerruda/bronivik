package service

import (
	"context"

	"bronivik/internal/config"
	"bronivik/internal/domain"
	"bronivik/internal/models"

	"github.com/rs/zerolog"
)

type UserService struct {
	repo         domain.Repository
	config       *config.Config
	logger       *zerolog.Logger
	managersMap  map[int64]bool
	blacklistMap map[int64]bool
}

func NewUserService(repo domain.Repository, config *config.Config, logger *zerolog.Logger) *UserService {
	managersMap := make(map[int64]bool)
	for _, id := range config.Managers {
		managersMap[id] = true
	}

	blacklistMap := make(map[int64]bool)
	for _, id := range config.Blacklist {
		blacklistMap[id] = true
	}

	return &UserService{
		repo:         repo,
		config:       config,
		logger:       logger,
		managersMap:  managersMap,
		blacklistMap: blacklistMap,
	}
}

func (s *UserService) IsManager(userID int64) bool {
	return s.managersMap[userID]
}

func (s *UserService) IsBlacklisted(userID int64) bool {
	return s.blacklistMap[userID]
}

func (s *UserService) SaveUser(ctx context.Context, user *models.User) error {
	user.IsManager = s.IsManager(user.TelegramID)
	user.IsBlacklisted = s.IsBlacklisted(user.TelegramID)
	return s.repo.CreateOrUpdateUser(ctx, user)
}

func (s *UserService) UpdateUserPhone(ctx context.Context, telegramID int64, phone string) error {
	return s.repo.UpdateUserPhone(ctx, telegramID, phone)
}

func (s *UserService) UpdateUserActivity(ctx context.Context, telegramID int64) error {
	return s.repo.UpdateUserActivity(ctx, telegramID)
}

func (s *UserService) GetAllUsers(ctx context.Context) ([]*models.User, error) {
	return s.repo.GetAllUsers(ctx)
}

func (s *UserService) GetActiveUsers(ctx context.Context, days int) ([]*models.User, error) {
	return s.repo.GetActiveUsers(ctx, days)
}

func (s *UserService) GetManagers(ctx context.Context) ([]*models.User, error) {
	return s.repo.GetUsersByManagerStatus(ctx, true)
}

func (s *UserService) GetUserBookings(ctx context.Context, userID int64) ([]*models.Booking, error) {
	return s.repo.GetUserBookings(ctx, userID)
}

func (s *UserService) GetUserByID(ctx context.Context, id int64) (*models.User, error) {
	return s.repo.GetUserByID(ctx, id)
}
