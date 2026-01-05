package service

import (
	"context"
	"testing"
	"time"

	"bronivik/internal/config"
	"bronivik/internal/models"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockRepository is a mock of the domain.Repository interface
type MockRepository struct {
	mock.Mock
}

func (m *MockRepository) GetBooking(ctx context.Context, id int64) (*models.Booking, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Booking), args.Error(1)
}

func (m *MockRepository) CreateBooking(ctx context.Context, booking *models.Booking) error {
	args := m.Called(ctx, booking)
	return args.Error(0)
}

func (m *MockRepository) CreateBookingWithLock(ctx context.Context, booking *models.Booking) error {
	args := m.Called(ctx, booking)
	return args.Error(0)
}

func (m *MockRepository) UpdateBookingStatus(ctx context.Context, id int64, status string) error {
	args := m.Called(ctx, id, status)
	return args.Error(0)
}

func (m *MockRepository) UpdateBookingStatusWithVersion(ctx context.Context, id int64, version int64, status string) error {
	args := m.Called(ctx, id, version, status)
	return args.Error(0)
}

func (m *MockRepository) GetBookingsByDateRange(ctx context.Context, start, end time.Time) ([]models.Booking, error) {
	args := m.Called(ctx, start, end)
	return args.Get(0).([]models.Booking), args.Error(1)
}

func (m *MockRepository) CheckAvailability(ctx context.Context, itemID int64, date time.Time) (bool, error) {
	args := m.Called(ctx, itemID, date)
	return args.Bool(0), args.Error(1)
}

func (m *MockRepository) GetAvailabilityForPeriod(ctx context.Context, itemID int64, startDate time.Time, days int) ([]models.Availability, error) {
	args := m.Called(ctx, itemID, startDate, days)
	return args.Get(0).([]models.Availability), args.Error(1)
}

func (m *MockRepository) GetActiveItems(ctx context.Context) ([]models.Item, error) {
	args := m.Called(ctx)
	return args.Get(0).([]models.Item), args.Error(1)
}

func (m *MockRepository) GetItemByName(ctx context.Context, name string) (*models.Item, error) {
	args := m.Called(ctx, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Item), args.Error(1)
}

func (m *MockRepository) CreateItem(ctx context.Context, item *models.Item) error {
	args := m.Called(ctx, item)
	return args.Error(0)
}

func (m *MockRepository) UpdateItem(ctx context.Context, item *models.Item) error {
	args := m.Called(ctx, item)
	return args.Error(0)
}

func (m *MockRepository) DeactivateItem(ctx context.Context, id int64) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockRepository) ReorderItem(ctx context.Context, id int64, newOrder int64) error {
	args := m.Called(ctx, id, newOrder)
	return args.Error(0)
}

func (m *MockRepository) GetAllUsers(ctx context.Context) ([]models.User, error) {
	args := m.Called(ctx)
	return args.Get(0).([]models.User), args.Error(1)
}

func (m *MockRepository) GetUserByTelegramID(ctx context.Context, telegramID int64) (*models.User, error) {
	args := m.Called(ctx, telegramID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.User), args.Error(1)
}

func (m *MockRepository) GetUserByID(ctx context.Context, id int64) (*models.User, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.User), args.Error(1)
}

func (m *MockRepository) CreateOrUpdateUser(ctx context.Context, user *models.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *MockRepository) UpdateUserActivity(ctx context.Context, telegramID int64) error {
	args := m.Called(ctx, telegramID)
	return args.Error(0)
}

func (m *MockRepository) UpdateUserPhone(ctx context.Context, telegramID int64, phone string) error {
	args := m.Called(ctx, telegramID, phone)
	return args.Error(0)
}

func (m *MockRepository) GetDailyBookings(ctx context.Context, start, end time.Time) (map[string][]models.Booking, error) {
	args := m.Called(ctx, start, end)
	return args.Get(0).(map[string][]models.Booking), args.Error(1)
}

func (m *MockRepository) GetBookedCount(ctx context.Context, itemID int64, date time.Time) (int, error) {
	args := m.Called(ctx, itemID, date)
	return args.Int(0), args.Error(1)
}

func (m *MockRepository) GetBookingWithAvailability(ctx context.Context, id int64, newItemID int64) (*models.Booking, bool, error) {
	args := m.Called(ctx, id, newItemID)
	if args.Get(0) == nil {
		return nil, args.Bool(1), args.Error(2)
	}
	return args.Get(0).(*models.Booking), args.Bool(1), args.Error(2)
}

func (m *MockRepository) UpdateBookingItemAndStatusWithVersion(ctx context.Context, id int64, version int64, itemID int64, itemName string, status string) error {
	args := m.Called(ctx, id, version, itemID, itemName, status)
	return args.Error(0)
}

func (m *MockRepository) SetItems(items []models.Item) {
	m.Called(items)
}

func (m *MockRepository) GetActiveUsers(ctx context.Context, days int) ([]models.User, error) {
	args := m.Called(ctx, days)
	return args.Get(0).([]models.User), args.Error(1)
}

func (m *MockRepository) GetUsersByManagerStatus(ctx context.Context, isManager bool) ([]models.User, error) {
	args := m.Called(ctx, isManager)
	return args.Get(0).([]models.User), args.Error(1)
}

func (m *MockRepository) GetUserBookings(ctx context.Context, userID int64) ([]models.Booking, error) {
	args := m.Called(ctx, userID)
	return args.Get(0).([]models.Booking), args.Error(1)
}

func TestUserService_IsManager(t *testing.T) {
	mockRepo := new(MockRepository)
	logger := zerolog.Nop()
	cfg := &config.Config{
		Managers: []int64{123, 456},
	}

	s := NewUserService(mockRepo, cfg, &logger)

	assert.True(t, s.IsManager(123))
	assert.True(t, s.IsManager(456))
	assert.False(t, s.IsManager(789))
	assert.False(t, s.IsManager(111))
}

func TestUserService_IsBlacklisted(t *testing.T) {
	mockRepo := new(MockRepository)
	logger := zerolog.Nop()
	cfg := &config.Config{
		Managers:  []int64{123},
		Blacklist: []int64{789, 999},
	}

	s := NewUserService(mockRepo, cfg, &logger)

	assert.True(t, s.IsBlacklisted(789))
	assert.True(t, s.IsBlacklisted(999))
	assert.False(t, s.IsBlacklisted(123))
	assert.False(t, s.IsBlacklisted(111))
}

func TestUserService_SaveUser(t *testing.T) {
	mockRepo := new(MockRepository)
	logger := zerolog.Nop()
	cfg := &config.Config{}
	s := NewUserService(mockRepo, cfg, &logger)

	user := &models.User{TelegramID: 123, FirstName: "Test"}

	mockRepo.On("CreateOrUpdateUser", mock.Anything, user).Return(nil)

	err := s.SaveUser(context.Background(), user)
	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}
