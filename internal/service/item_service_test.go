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

	mockRepo.On("GetActiveItems", mock.Anything).Return(items, nil)

	s := NewItemService(mockRepo, &logger)

	res, err := s.GetActiveItems(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, items, res)
	mockRepo.AssertExpectations(t)
}

func TestItemService_GetItemByID(t *testing.T) {
	mockRepo := new(MockRepository)
	logger := zerolog.Nop()
	item1 := &models.Item{ID: 1, Name: "Item 1"}

	mockRepo.On("GetItemByID", mock.Anything, int64(1)).Return(item1, nil)
	mockRepo.On("GetItemByID", mock.Anything, int64(2)).Return(nil, assert.AnError)

	s := NewItemService(mockRepo, &logger)

	item, err := s.GetItemByID(context.Background(), 1)
	assert.NoError(t, err)
	assert.Equal(t, "Item 1", item.Name)

	item, err = s.GetItemByID(context.Background(), 2)
	assert.Error(t, err)
	assert.Nil(t, item)
	mockRepo.AssertExpectations(t)
}

func TestItemService_Refresh(t *testing.T) {
	mockRepo := new(MockRepository)
	logger := zerolog.Nop()

	s := NewItemService(mockRepo, &logger)

	err := s.Refresh(context.Background())
	assert.NoError(t, err)
}
