package service

import (
	"context"
	"testing"

	"bronivik/internal/models"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestItemService_GetActiveItems(t *testing.T) {
	mockRepo := new(MockRepository)
	logger := zerolog.Nop()
	items := []models.Item{
		{ID: 1, Name: "Item 1"},
		{ID: 2, Name: "Item 2"},
	}

	s := NewItemService(mockRepo, items, &logger)

	res, err := s.GetActiveItems(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, items, res)
}

func TestItemService_GetItemByID(t *testing.T) {
	mockRepo := new(MockRepository)
	logger := zerolog.Nop()
	items := []models.Item{
		{ID: 1, Name: "Item 1"},
	}

	s := NewItemService(mockRepo, items, &logger)

	item, err := s.GetItemByID(context.Background(), 1)
	assert.NoError(t, err)
	assert.Equal(t, "Item 1", item.Name)

	item, err = s.GetItemByID(context.Background(), 2)
	assert.Error(t, err)
	assert.Nil(t, item)
}

func TestItemService_Refresh(t *testing.T) {
	mockRepo := new(MockRepository)
	logger := zerolog.Nop()
	initialItems := []models.Item{
		{ID: 1, Name: "Item 1"},
	}

	s := NewItemService(mockRepo, initialItems, &logger)

	newItems := []models.Item{
		{ID: 1, Name: "Item 1 Updated"},
		{ID: 2, Name: "Item 2"},
	}

	mockRepo.On("GetActiveItems", mock.Anything).Return(newItems, nil)

	err := s.Refresh(context.Background())
	assert.NoError(t, err)

	res, _ := s.GetActiveItems(context.Background())
	assert.Equal(t, newItems, res)
	mockRepo.AssertExpectations(t)
}
