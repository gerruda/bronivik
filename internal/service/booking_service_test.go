package service

import (
	"context"
	"testing"
	"time"

	"bronivik/internal/models"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockEventPublisher is a mock of the domain.EventPublisher interface
type MockEventPublisher struct {
	mock.Mock
}

func (m *MockEventPublisher) PublishJSON(eventType string, payload interface{}) error {
	args := m.Called(eventType, payload)
	return args.Error(0)
}

// MockSyncWorker is a mock of the domain.SyncWorker interface
type MockSyncWorker struct {
	mock.Mock
}

func (m *MockSyncWorker) EnqueueTask(ctx context.Context, taskType string, bookingID int64, booking *models.Booking, status string) error {
	args := m.Called(ctx, taskType, bookingID, booking, status)
	return args.Error(0)
}

func (m *MockSyncWorker) EnqueueSyncSchedule(ctx context.Context, startDate, endDate time.Time) error {
	args := m.Called(ctx, startDate, endDate)
	return args.Error(0)
}

func TestBookingService_CreateBooking(t *testing.T) {
	mockRepo := new(MockRepository)
	mockEvents := new(MockEventPublisher)
	mockWorker := new(MockSyncWorker)
	logger := zerolog.Nop()
	s := NewBookingService(mockRepo, mockEvents, mockWorker, 365, &logger)

	booking := &models.Booking{
		ID:     1,
		ItemID: 1,
		Date:   time.Now(),
	}

	mockRepo.On("CheckAvailability", mock.Anything, booking.ItemID, booking.Date).Return(true, nil)
	mockRepo.On("CreateBookingWithLock", mock.Anything, booking).Return(nil)
	mockEvents.On("PublishJSON", mock.Anything, mock.Anything).Return(nil)
	mockWorker.On("EnqueueTask", mock.Anything, "upsert", booking.ID, booking, "").Return(nil)
	mockWorker.On("EnqueueSyncSchedule", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	err := s.CreateBooking(context.Background(), booking)
	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockEvents.AssertExpectations(t)
	mockWorker.AssertExpectations(t)
}

func TestBookingService_ConfirmBooking(t *testing.T) {
	mockRepo := new(MockRepository)
	mockEvents := new(MockEventPublisher)
	mockWorker := new(MockSyncWorker)
	logger := zerolog.Nop()
	s := NewBookingService(mockRepo, mockEvents, mockWorker, 365, &logger)

	bookingID := int64(1)
	version := int64(1)
	managerID := int64(123)
	booking := &models.Booking{ID: bookingID, Status: models.StatusConfirmed}

	mockRepo.On("UpdateBookingStatusWithVersion", mock.Anything, bookingID, version, models.StatusConfirmed).Return(nil)
	mockRepo.On("GetBooking", mock.Anything, bookingID).Return(booking, nil)
	mockEvents.On("PublishJSON", mock.Anything, mock.Anything).Return(nil)
	mockWorker.On("EnqueueTask", mock.Anything, "update_status", bookingID, booking, models.StatusConfirmed).Return(nil)
	mockWorker.On("EnqueueSyncSchedule", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	err := s.ConfirmBooking(context.Background(), bookingID, version, managerID)
	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

func TestBookingService_CheckAvailability(t *testing.T) {
	mockRepo := new(MockRepository)
	logger := zerolog.Nop()
	s := NewBookingService(mockRepo, nil, nil, 365, &logger)

	itemID := int64(1)
	date := time.Now()

	mockRepo.On("CheckAvailability", mock.Anything, itemID, date).Return(true, nil)

	available, err := s.CheckAvailability(context.Background(), itemID, date)
	assert.NoError(t, err)
	assert.True(t, available)
	mockRepo.AssertExpectations(t)
}

func TestBookingService_ValidateBookingDate(t *testing.T) {
	logger := zerolog.Nop()
	s := NewBookingService(nil, nil, nil, 30, &logger)

	t.Run("ValidDate", func(t *testing.T) {
		err := s.ValidateBookingDate(time.Now().AddDate(0, 0, 5))
		assert.NoError(t, err)
	})

	t.Run("PastDate", func(t *testing.T) {
		err := s.ValidateBookingDate(time.Now().AddDate(0, 0, -2))
		assert.Error(t, err)
	})

	t.Run("TooFarDate", func(t *testing.T) {
		err := s.ValidateBookingDate(time.Now().AddDate(0, 0, 40))
		assert.Error(t, err)
	})
}
