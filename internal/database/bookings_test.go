package database

import (
	"context"
	"os"
	"testing"
	"time"

	"bronivik/internal/models"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) *DB {
	logger := zerolog.New(os.Stdout)
	db, err := NewDB(":memory:", &logger)
	require.NoError(t, err)
	return db
}

func TestGetAvailabilityForPeriod(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// Create test item
	item := &models.Item{
		Name:          "Test Item",
		TotalQuantity: 2,
		IsActive:      true,
	}
	err := db.CreateItem(ctx, item)
	require.NoError(t, err)

	// Get the created item to have its ID
	items, err := db.GetActiveItems(ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)
	itemID := items[0].ID

	startDate := time.Now().Truncate(24 * time.Hour)

	// Create some bookings
	// Date 0: 2 bookings (Full)
	// Date 1: 1 booking (1 available)
	// Date 2: 0 bookings (2 available)

	bookings := []models.Booking{
		{ItemID: itemID, ItemName: item.Name, Date: startDate, UserID: 1, UserName: "User 1", Phone: "123", Status: models.StatusConfirmed},
		{ItemID: itemID, ItemName: item.Name, Date: startDate, UserID: 2, UserName: "User 2", Phone: "456", Status: models.StatusConfirmed},
		{ItemID: itemID, ItemName: item.Name, Date: startDate.AddDate(0, 0, 1), UserID: 3, UserName: "User 3", Phone: "789", Status: models.StatusConfirmed},
	}

	for _, b := range bookings {
		err := db.CreateBooking(ctx, &b)
		require.NoError(t, err)
	}

	// Test GetAvailabilityForPeriod
	availability, err := db.GetAvailabilityForPeriod(ctx, itemID, startDate, 3)
	require.NoError(t, err)
	require.Len(t, availability, 3)

	// Date 0
	assert.Equal(t, startDate, availability[0].Date)
	assert.Equal(t, int64(0), availability[0].Available)
	assert.Equal(t, int64(2), availability[0].Booked)

	// Date 1
	assert.Equal(t, startDate.AddDate(0, 0, 1), availability[1].Date)
	assert.Equal(t, int64(1), availability[1].Available)
	assert.Equal(t, int64(1), availability[1].Booked)

	// Date 2
	assert.Equal(t, startDate.AddDate(0, 0, 2), availability[2].Date)
	assert.Equal(t, int64(2), availability[2].Available)
	assert.Equal(t, int64(0), availability[2].Booked)
}

func TestCheckAvailability(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	item := &models.Item{
		Name:          "Test Item",
		TotalQuantity: 1,
		IsActive:      true,
	}
	err := db.CreateItem(ctx, item)
	require.NoError(t, err)

	items, _ := db.GetActiveItems(ctx)
	itemID := items[0].ID

	date := time.Now().Truncate(24 * time.Hour)

	// Initially available
	available, err := db.CheckAvailability(ctx, itemID, date)
	require.NoError(t, err)
	assert.True(t, available)

	// Create booking
	booking := &models.Booking{
		ItemID: itemID, ItemName: item.Name, Date: date, UserID: 1, UserName: "User 1", Phone: "123", Status: models.StatusConfirmed,
	}
	err = db.CreateBooking(ctx, booking)
	require.NoError(t, err)

	// Now unavailable
	available, err = db.CheckAvailability(ctx, itemID, date)
	require.NoError(t, err)
	assert.False(t, available)
}

func TestOptimisticLocking(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// Create booking
	booking := &models.Booking{
		ItemID: 1, ItemName: "Test", Date: time.Now(), UserID: 1, UserName: "User 1", Phone: "123", Status: models.StatusPending,
	}
	err := db.CreateBooking(ctx, booking)
	require.NoError(t, err)
	assert.Equal(t, int64(1), booking.Version)

	// Successful update
	err = db.UpdateBookingStatusWithVersion(ctx, booking.ID, booking.Version, models.StatusConfirmed)
	require.NoError(t, err)

	// Failed update with old version
	err = db.UpdateBookingStatusWithVersion(ctx, booking.ID, booking.Version, models.StatusCancelled)
	assert.ErrorIs(t, err, ErrConcurrentModification)

	// Successful update with new version
	updated, _ := db.GetBooking(ctx, booking.ID)
	assert.Equal(t, int64(2), updated.Version)
	err = db.UpdateBookingStatusWithVersion(ctx, updated.ID, updated.Version, models.StatusCancelled)
	require.NoError(t, err)
}
