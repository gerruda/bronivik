package service

import (
	"context"
	"fmt"
	"sync"

	"bronivik/internal/domain"
	"bronivik/internal/models"

	"github.com/rs/zerolog"
)

type ItemService struct {
	repo     domain.Repository
	logger   *zerolog.Logger
	items    []models.Item
	itemsMap map[int64]models.Item
	mu       sync.RWMutex
}

func NewItemService(repo domain.Repository, items []models.Item, logger *zerolog.Logger) *ItemService {
	itemsMap := make(map[int64]models.Item)
	for _, item := range items {
		itemsMap[item.ID] = item
	}

	return &ItemService{
		repo:     repo,
		logger:   logger,
		items:    items,
		itemsMap: itemsMap,
	}
}

func (s *ItemService) GetActiveItems(ctx context.Context) ([]models.Item, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.items, nil
}

func (s *ItemService) GetItemByID(ctx context.Context, id int64) (*models.Item, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.itemsMap[id]
	if !ok {
		return nil, fmt.Errorf("item not found: %d", id)
	}
	return &item, nil
}

func (s *ItemService) GetItemByName(ctx context.Context, name string) (*models.Item, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.items {
		if item.Name == name {
			return &item, nil
		}
	}
	return nil, fmt.Errorf("item not found: %s", name)
}

func (s *ItemService) CreateItem(ctx context.Context, item *models.Item) error {
	err := s.repo.CreateItem(ctx, item)
	if err != nil {
		return err
	}
	return s.Refresh(ctx)
}

func (s *ItemService) UpdateItem(ctx context.Context, item *models.Item) error {
	err := s.repo.UpdateItem(ctx, item)
	if err != nil {
		return err
	}
	return s.Refresh(ctx)
}

func (s *ItemService) DeactivateItem(ctx context.Context, id int64) error {
	err := s.repo.DeactivateItem(ctx, id)
	if err != nil {
		return err
	}
	return s.Refresh(ctx)
}

func (s *ItemService) ReorderItem(ctx context.Context, id int64, newOrder int64) error {
	err := s.repo.ReorderItem(ctx, id, newOrder)
	if err != nil {
		return err
	}
	return s.Refresh(ctx)
}

func (s *ItemService) Refresh(ctx context.Context) error {
	items, err := s.repo.GetActiveItems(ctx)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = items
	s.itemsMap = make(map[int64]models.Item)
	for _, item := range items {
		s.itemsMap[item.ID] = item
	}
	return nil
}
