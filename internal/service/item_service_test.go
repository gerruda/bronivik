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
	items := []*models.Item{
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

func TestItemService_GetItemByName(t *testing.T) {
	mockRepo := new(MockRepository)
	logger := zerolog.Nop()
	item := &models.Item{ID: 1, Name: "Test Item"}

	mockRepo.On("GetItemByName", mock.Anything, "Test Item").Return(item, nil)
	mockRepo.On("GetItemByName", mock.Anything, "Unknown").Return(nil, assert.AnError)

	s := NewItemService(mockRepo, &logger)

	result, err := s.GetItemByName(context.Background(), "Test Item")
	assert.NoError(t, err)
	assert.Equal(t, item, result)

	result, err = s.GetItemByName(context.Background(), "Unknown")
	assert.Error(t, err)
	assert.Nil(t, result)
	mockRepo.AssertExpectations(t)
}

func TestItemService_CreateItem(t *testing.T) {
	mockRepo := new(MockRepository)
	logger := zerolog.Nop()
	item := &models.Item{Name: "New Item"}

	mockRepo.On("CreateItem", mock.Anything, item).Return(nil)

	s := NewItemService(mockRepo, &logger)

	err := s.CreateItem(context.Background(), item)
	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

func TestItemService_UpdateItem(t *testing.T) {
	mockRepo := new(MockRepository)
	logger := zerolog.Nop()
	item := &models.Item{ID: 1, Name: "Updated Item"}

	mockRepo.On("UpdateItem", mock.Anything, item).Return(nil)

	s := NewItemService(mockRepo, &logger)

	err := s.UpdateItem(context.Background(), item)
	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

func TestItemService_DeactivateItem(t *testing.T) {
	mockRepo := new(MockRepository)
	logger := zerolog.Nop()

	mockRepo.On("DeactivateItem", mock.Anything, int64(1)).Return(nil)

	s := NewItemService(mockRepo, &logger)

	err := s.DeactivateItem(context.Background(), 1)
	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

func TestItemService_ReorderItem(t *testing.T) {
	mockRepo := new(MockRepository)
	logger := zerolog.Nop()

	mockRepo.On("ReorderItem", mock.Anything, int64(1), int64(5)).Return(nil)

	s := NewItemService(mockRepo, &logger)

	err := s.ReorderItem(context.Background(), 1, 5)
	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}
