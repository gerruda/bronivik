package service

import (
	"context"

	"bronivik/internal/domain"
	"bronivik/internal/models"

	"github.com/rs/zerolog"
)

type ItemService struct {
	repo   domain.Repository
	logger *zerolog.Logger
}

func NewItemService(repo domain.Repository, logger *zerolog.Logger) *ItemService {
	return &ItemService{
		repo:   repo,
		logger: logger,
	}
}

func (s *ItemService) GetActiveItems(ctx context.Context) ([]*models.Item, error) {
	return s.repo.GetActiveItems(ctx)
}

func (s *ItemService) GetItemByID(ctx context.Context, id int64) (*models.Item, error) {
	return s.repo.GetItemByID(ctx, id)
}

func (s *ItemService) GetItemByName(ctx context.Context, name string) (*models.Item, error) {
	return s.repo.GetItemByName(ctx, name)
}

func (s *ItemService) CreateItem(ctx context.Context, item *models.Item) error {
	return s.repo.CreateItem(ctx, item)
}

func (s *ItemService) UpdateItem(ctx context.Context, item *models.Item) error {
	return s.repo.UpdateItem(ctx, item)
}

func (s *ItemService) DeactivateItem(ctx context.Context, id int64) error {
	return s.repo.DeactivateItem(ctx, id)
}

func (s *ItemService) ReorderItem(ctx context.Context, id, newOrder int64) error {
	return s.repo.ReorderItem(ctx, id, newOrder)
}

func (s *ItemService) Refresh(ctx context.Context) error {
	// No-op now as repo handles caching
	return nil
}
