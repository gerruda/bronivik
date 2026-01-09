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

func (m *MockRepository) UpdateBookingStatusWithVersion(ctx context.Context, id, version int64, status string) error {
	args := m.Called(ctx, id, version, status)
	return args.Error(0)
}

func (m *MockRepository) GetBookingsByDateRange(ctx context.Context, start, end time.Time) ([]*models.Booking, error) {
	args := m.Called(ctx, start, end)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.Booking), args.Error(1)
}

func (m *MockRepository) CheckAvailability(ctx context.Context, itemID int64, date time.Time) (bool, error) {
	args := m.Called(ctx, itemID, date)
	return args.Bool(0), args.Error(1)
}

func (m *MockRepository) GetAvailabilityForPeriod(
	ctx context.Context,
	itemID int64,
	startDate time.Time,
	days int,
) ([]*models.Availability, error) {
	args := m.Called(ctx, itemID, startDate, days)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.Availability), args.Error(1)
}

func (m *MockRepository) GetActiveItems(ctx context.Context) ([]*models.Item, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.Item), args.Error(1)
}

func (m *MockRepository) GetItemByID(ctx context.Context, id int64) (*models.Item, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Item), args.Error(1)
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

func (m *MockRepository) ReorderItem(ctx context.Context, id, newOrder int64) error {
	args := m.Called(ctx, id, newOrder)
	return args.Error(0)
}

func (m *MockRepository) GetAllUsers(ctx context.Context) ([]*models.User, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.User), args.Error(1)
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

func (m *MockRepository) GetDailyBookings(ctx context.Context, start, end time.Time) (map[string][]*models.Booking, error) {
	args := m.Called(ctx, start, end)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string][]*models.Booking), args.Error(1)
}

func (m *MockRepository) GetBookedCount(ctx context.Context, itemID int64, date time.Time) (int, error) {
	args := m.Called(ctx, itemID, date)
	return args.Int(0), args.Error(1)
}

func (m *MockRepository) GetBookingWithAvailability(ctx context.Context, id, newItemID int64) (*models.Booking, bool, error) {
	args := m.Called(ctx, id, newItemID)
	if args.Get(0) == nil {
		return nil, args.Bool(1), args.Error(2)
	}
	return args.Get(0).(*models.Booking), args.Bool(1), args.Error(2)
}

func (m *MockRepository) UpdateBookingItemAndStatusWithVersion(
	ctx context.Context,
	id, version, itemID int64,
	itemName, status string,
) error {
	args := m.Called(ctx, id, version, itemID, itemName, status)
	return args.Error(0)
}

func (m *MockRepository) SetItems(items []*models.Item) {
	m.Called(items)
}

func (m *MockRepository) GetActiveUsers(ctx context.Context, days int) ([]*models.User, error) {
	args := m.Called(ctx, days)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.User), args.Error(1)
}

func (m *MockRepository) GetUsersByManagerStatus(ctx context.Context, isManager bool) ([]*models.User, error) {
	args := m.Called(ctx, isManager)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.User), args.Error(1)
}

func (m *MockRepository) GetUserBookings(ctx context.Context, userID int64) ([]*models.Booking, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.Booking), args.Error(1)
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

func TestUserService_UpdateUserPhone(t *testing.T) {
	mockRepo := new(MockRepository)
	logger := zerolog.Nop()
	cfg := &config.Config{}
	s := NewUserService(mockRepo, cfg, &logger)

	mockRepo.On("UpdateUserPhone", mock.Anything, int64(123), "+79991234567").Return(nil)

	err := s.UpdateUserPhone(context.Background(), 123, "+79991234567")
	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

func TestUserService_UpdateUserActivity(t *testing.T) {
	mockRepo := new(MockRepository)
	logger := zerolog.Nop()
	cfg := &config.Config{}
	s := NewUserService(mockRepo, cfg, &logger)

	mockRepo.On("UpdateUserActivity", mock.Anything, int64(123)).Return(nil)

	err := s.UpdateUserActivity(context.Background(), 123)
	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

func TestUserService_GetAllUsers(t *testing.T) {
	mockRepo := new(MockRepository)
	logger := zerolog.Nop()
	cfg := &config.Config{}
	s := NewUserService(mockRepo, cfg, &logger)

	users := []*models.User{{ID: 1}, {ID: 2}}
	mockRepo.On("GetAllUsers", mock.Anything).Return(users, nil)

	result, err := s.GetAllUsers(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, users, result)
	mockRepo.AssertExpectations(t)
}

func TestUserService_GetActiveUsers(t *testing.T) {
	mockRepo := new(MockRepository)
	logger := zerolog.Nop()
	cfg := &config.Config{}
	s := NewUserService(mockRepo, cfg, &logger)

	users := []*models.User{{ID: 1}, {ID: 2}}
	mockRepo.On("GetActiveUsers", mock.Anything, 7).Return(users, nil)

	result, err := s.GetActiveUsers(context.Background(), 7)
	assert.NoError(t, err)
	assert.Equal(t, users, result)
	mockRepo.AssertExpectations(t)
}

func TestUserService_GetManagers(t *testing.T) {
	mockRepo := new(MockRepository)
	logger := zerolog.Nop()
	cfg := &config.Config{}
	s := NewUserService(mockRepo, cfg, &logger)

	users := []*models.User{{ID: 1, IsManager: true}, {ID: 2, IsManager: true}}
	mockRepo.On("GetUsersByManagerStatus", mock.Anything, true).Return(users, nil)

	result, err := s.GetManagers(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, users, result)
	mockRepo.AssertExpectations(t)
}

func TestUserService_GetUserBookings(t *testing.T) {
	mockRepo := new(MockRepository)
	logger := zerolog.Nop()
	cfg := &config.Config{}
	s := NewUserService(mockRepo, cfg, &logger)

	bookings := []*models.Booking{{ID: 1}, {ID: 2}}
	mockRepo.On("GetUserBookings", mock.Anything, int64(123)).Return(bookings, nil)

	result, err := s.GetUserBookings(context.Background(), 123)
	assert.NoError(t, err)
	assert.Equal(t, bookings, result)
	mockRepo.AssertExpectations(t)
}

func TestUserService_GetUserByID(t *testing.T) {
	mockRepo := new(MockRepository)
	logger := zerolog.Nop()
	cfg := &config.Config{}
	s := NewUserService(mockRepo, cfg, &logger)

	user := &models.User{ID: 1, FirstName: "Test"}
	mockRepo.On("GetUserByID", mock.Anything, int64(1)).Return(user, nil)
	mockRepo.On("GetUserByID", mock.Anything, int64(2)).Return(nil, assert.AnError)

	result, err := s.GetUserByID(context.Background(), 1)
	assert.NoError(t, err)
	assert.Equal(t, user, result)

	result, err = s.GetUserByID(context.Background(), 2)
	assert.Error(t, err)
	assert.Nil(t, result)
	mockRepo.AssertExpectations(t)
}
