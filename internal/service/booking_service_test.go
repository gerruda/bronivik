package service

import (
	"context"
	"io"
	"testing"
	"time"

	"bronivik/internal/models"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockRepo struct {
	mock.Mock
}

func (m *mockRepo) GetBooking(ctx context.Context, id int64) (*models.Booking, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Booking), args.Error(1)
}
func (m *mockRepo) CreateBooking(ctx context.Context, b *models.Booking) error {
	return m.Called(ctx, b).Error(0)
}
func (m *mockRepo) CreateBookingWithLock(ctx context.Context, b *models.Booking) error {
	return m.Called(ctx, b).Error(0)
}
func (m *mockRepo) UpdateBookingStatus(ctx context.Context, id int64, s string) error {
	return m.Called(ctx, id, s).Error(0)
}
func (m *mockRepo) UpdateBookingStatusWithVersion(ctx context.Context, id, v int64, s string) error {
	return m.Called(ctx, id, v, s).Error(0)
}
func (m *mockRepo) GetBookingsByDateRange(ctx context.Context, s, e time.Time) ([]*models.Booking, error) {
	args := m.Called(ctx, s, e)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.Booking), args.Error(1)
}
func (m *mockRepo) CheckAvailability(ctx context.Context, id int64, d time.Time) (bool, error) {
	args := m.Called(ctx, id, d)
	return args.Bool(0), args.Error(1)
}
func (m *mockRepo) GetAvailabilityForPeriod(ctx context.Context, id int64, s time.Time, d int) ([]*models.Availability, error) {
	args := m.Called(ctx, id, s, d)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.Availability), args.Error(1)
}
func (m *mockRepo) GetActiveItems(ctx context.Context) ([]*models.Item, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.Item), args.Error(1)
}
func (m *mockRepo) GetItemByID(ctx context.Context, id int64) (*models.Item, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Item), args.Error(1)
}
func (m *mockRepo) GetItemByName(ctx context.Context, n string) (*models.Item, error) {
	args := m.Called(ctx, n)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Item), args.Error(1)
}
func (m *mockRepo) CreateItem(ctx context.Context, i *models.Item) error {
	return m.Called(ctx, i).Error(0)
}
func (m *mockRepo) UpdateItem(ctx context.Context, i *models.Item) error {
	return m.Called(ctx, i).Error(0)
}
func (m *mockRepo) DeactivateItem(ctx context.Context, id int64) error {
	return m.Called(ctx, id).Error(0)
}
func (m *mockRepo) ReorderItem(ctx context.Context, id, o int64) error {
	return m.Called(ctx, id, o).Error(0)
}
func (m *mockRepo) GetAllUsers(ctx context.Context) ([]*models.User, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.User), args.Error(1)
}
func (m *mockRepo) GetUserByTelegramID(ctx context.Context, id int64) (*models.User, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.User), args.Error(1)
}
func (m *mockRepo) GetUserByID(ctx context.Context, id int64) (*models.User, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.User), args.Error(1)
}
func (m *mockRepo) CreateOrUpdateUser(ctx context.Context, u *models.User) error {
	return m.Called(ctx, u).Error(0)
}
func (m *mockRepo) UpdateUserActivity(ctx context.Context, id int64) error {
	return m.Called(ctx, id).Error(0)
}
func (m *mockRepo) UpdateUserPhone(ctx context.Context, id int64, p string) error {
	return m.Called(ctx, id, p).Error(0)
}
func (m *mockRepo) GetDailyBookings(ctx context.Context, s, e time.Time) (map[string][]*models.Booking, error) {
	args := m.Called(ctx, s, e)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string][]*models.Booking), args.Error(1)
}
func (m *mockRepo) GetBookedCount(ctx context.Context, id int64, d time.Time) (int, error) {
	args := m.Called(ctx, id, d)
	return args.Int(0), args.Error(1)
}
func (m *mockRepo) GetBookingWithAvailability(ctx context.Context, id, nid int64) (*models.Booking, bool, error) {
	args := m.Called(ctx, id, nid)
	if args.Get(0) == nil {
		return nil, args.Bool(1), args.Error(2)
	}
	return args.Get(0).(*models.Booking), args.Bool(1), args.Error(2)
}
func (m *mockRepo) UpdateBookingItemAndStatusWithVersion(ctx context.Context, id, v, iid int64, in, s string) error {
	return m.Called(ctx, id, v, iid, in, s).Error(0)
}
func (m *mockRepo) SetItems(items []*models.Item) { m.Called(items) }
func (m *mockRepo) GetActiveUsers(ctx context.Context, d int) ([]*models.User, error) {
	args := m.Called(ctx, d)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.User), args.Error(1)
}
func (m *mockRepo) GetUsersByManagerStatus(ctx context.Context, im bool) ([]*models.User, error) {
	args := m.Called(ctx, im)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.User), args.Error(1)
}
func (m *mockRepo) GetUserBookings(ctx context.Context, id int64) ([]*models.Booking, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.Booking), args.Error(1)
}

type mockEventBus struct {
	mock.Mock
}

func (m *mockEventBus) PublishJSON(et string, p interface{}) error { return m.Called(et, p).Error(0) }

type mockWorker struct {
	mock.Mock
}

func (m *mockWorker) EnqueueTask(ctx context.Context, tt string, bid int64, b *models.Booking, s string) error {
	return m.Called(ctx, tt, bid, b, s).Error(0)
}
func (m *mockWorker) EnqueueSyncSchedule(ctx context.Context, s, e time.Time) error {
	return m.Called(ctx, s, e).Error(0)
}

func TestBookingService(t *testing.T) {
	repo := new(mockRepo)
	bus := new(mockEventBus)
	worker := new(mockWorker)
	logger := zerolog.New(io.Discard)
	svc := NewBookingService(repo, bus, worker, 30, 2, &logger)
	ctx := context.Background()

	t.Run("ValidateBookingDate", func(t *testing.T) {
		now := time.Now()

		// Past date
		err := svc.ValidateBookingDate(now.AddDate(0, 0, -1))
		assert.Error(t, err)

		// Too far
		err = svc.ValidateBookingDate(now.AddDate(0, 0, 31))
		assert.Error(t, err)

		// Valid
		err = svc.ValidateBookingDate(now.AddDate(0, 0, 5))
		assert.NoError(t, err)
	})

	t.Run("CreateBooking", func(t *testing.T) {
		date := time.Now().AddDate(0, 0, 5)
		booking := &models.Booking{ItemID: 1, Date: date}

		repo.On("CheckAvailability", ctx, int64(1), date).Return(true, nil).Once()
		repo.On("CreateBookingWithLock", ctx, booking).Return(nil).Once()
		bus.On("PublishJSON", mock.Anything, mock.Anything).Return(nil).Once()
		worker.On("EnqueueTask", ctx, "upsert", int64(0), booking, "").Return(nil).Once()
		worker.On("EnqueueSyncSchedule", ctx, mock.Anything, mock.Anything).Return(nil).Once()

		err := svc.CreateBooking(ctx, booking)
		assert.NoError(t, err)
		repo.AssertExpectations(t)
	})

	testStatusUpdate := func(
		name string,
		bookingID int64,
		version int64,
		status string,
		method func(context.Context, int64, int64, int64) error,
	) {
		t.Run(name, func(t *testing.T) {
			booking := &models.Booking{ID: bookingID, Status: status}
			repo.On("UpdateBookingStatusWithVersion", ctx, bookingID, version, status).Return(nil).Once()
			repo.On("GetBooking", ctx, bookingID).Return(booking, nil).Once()
			bus.On("PublishJSON", mock.Anything, mock.Anything).Return(nil).Once()
			worker.On("EnqueueTask", ctx, "update_status", bookingID, booking, status).Return(nil).Once()
			worker.On("EnqueueSyncSchedule", ctx, mock.Anything, mock.Anything).Return(nil).Once()

			err := method(ctx, bookingID, version, 100)
			assert.NoError(t, err)
			repo.AssertExpectations(t)
		})
	}

	testStatusUpdate("ConfirmBooking", 10, 1, models.StatusConfirmed, svc.ConfirmBooking)
	testStatusUpdate("RejectBooking", 11, 2, models.StatusCanceled, svc.RejectBooking)
	testStatusUpdate("CompleteBooking", 12, 3, models.StatusCompleted, svc.CompleteBooking)
	testStatusUpdate("ReopenBooking", 13, 4, models.StatusPending, svc.ReopenBooking)

	t.Run("ChangeBookingItem", func(t *testing.T) {
		oldBooking := &models.Booking{ID: 14, ItemID: 1, ItemName: "Old Item", Status: models.StatusPending}
		newBooking := &models.Booking{ID: 14, ItemID: 2, ItemName: "New Item", Status: models.StatusChanged}
		items := []*models.Item{{ID: 2, Name: "New Item"}}

		repo.On("GetBookingWithAvailability", ctx, int64(14), int64(2)).Return(oldBooking, true, nil).Once()
		repo.On("GetActiveItems", ctx).Return(items, nil).Once()
		repo.On("UpdateBookingItemAndStatusWithVersion", ctx, int64(14), int64(5), int64(2), "New Item", models.StatusChanged).Return(nil).Once()
		repo.On("GetBooking", ctx, int64(14)).Return(newBooking, nil).Once()
		bus.On("PublishJSON", mock.Anything, mock.Anything).Return(nil).Once()
		worker.On("EnqueueTask", ctx, "upsert", int64(14), newBooking, "").Return(nil).Once()
		worker.On("EnqueueSyncSchedule", ctx, mock.Anything, mock.Anything).Return(nil).Once()

		err := svc.ChangeBookingItem(ctx, 14, 5, 2, 100)
		assert.NoError(t, err)
		repo.AssertExpectations(t)
	})

	t.Run("RescheduleBooking", func(t *testing.T) {
		booking := &models.Booking{ID: 15, Status: "rescheduled"}

		repo.On("UpdateBookingStatus", ctx, int64(15), "rescheduled").Return(nil).Once()
		repo.On("GetBooking", ctx, int64(15)).Return(booking, nil).Once()
		worker.On("EnqueueTask", ctx, "update_status", int64(15), booking, "rescheduled").Return(nil).Once()
		worker.On("EnqueueSyncSchedule", ctx, mock.Anything, mock.Anything).Return(nil).Once()

		err := svc.RescheduleBooking(ctx, 15, 100)
		assert.NoError(t, err)
		repo.AssertExpectations(t)
	})

	t.Run("GetAvailability", func(t *testing.T) {
		availabilities := []*models.Availability{{Date: time.Now(), ItemID: 1, Booked: 2, Available: 3}}

		repo.On("GetAvailabilityForPeriod", ctx, int64(1), mock.AnythingOfType("time.Time"), 7).Return(availabilities, nil).Once()

		result, err := svc.GetAvailability(ctx, 1, time.Now(), 7)
		assert.NoError(t, err)
		assert.Equal(t, availabilities, result)
		repo.AssertExpectations(t)
	})

	t.Run("CheckAvailability", func(t *testing.T) {
		repo.On("CheckAvailability", ctx, int64(2), mock.AnythingOfType("time.Time")).Return(false, nil).Once()

		available, err := svc.CheckAvailability(ctx, 2, time.Now())
		assert.NoError(t, err)
		assert.False(t, available)
		repo.AssertExpectations(t)
	})

	t.Run("GetBookedCount", func(t *testing.T) {
		repo.On("GetBookedCount", ctx, int64(3), mock.AnythingOfType("time.Time")).Return(5, nil).Once()

		count, err := svc.GetBookedCount(ctx, 3, time.Now())
		assert.NoError(t, err)
		assert.Equal(t, 5, count)
		repo.AssertExpectations(t)
	})

	t.Run("GetBookingsByDateRange", func(t *testing.T) {
		start := time.Now()
		end := start.AddDate(0, 0, 7)
		bookings := []*models.Booking{{ID: 1}, {ID: 2}}

		repo.On("GetBookingsByDateRange", ctx, start, end).Return(bookings, nil).Once()

		result, err := svc.GetBookingsByDateRange(ctx, start, end)
		assert.NoError(t, err)
		assert.Equal(t, bookings, result)
		repo.AssertExpectations(t)
	})

	t.Run("GetBooking", func(t *testing.T) {
		booking := &models.Booking{ID: 16}

		repo.On("GetBooking", ctx, int64(16)).Return(booking, nil).Once()

		result, err := svc.GetBooking(ctx, 16)
		assert.NoError(t, err)
		assert.Equal(t, booking, result)
		repo.AssertExpectations(t)
	})

	t.Run("GetDailyBookings", func(t *testing.T) {
		start := time.Now()
		end := start.AddDate(0, 0, 7)
		daily := map[string][]*models.Booking{"2025-01-01": {{ID: 1}}}

		repo.On("GetDailyBookings", ctx, start, end).Return(daily, nil).Once()

		result, err := svc.GetDailyBookings(ctx, start, end)
		assert.NoError(t, err)
		assert.Equal(t, daily, result)
		repo.AssertExpectations(t)
	})
}
