package database

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"bronivik/internal/models"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestConcurrentBooking(t *testing.T) {
	logger := zerolog.New(zerolog.NewConsoleWriter())
	dbPath := filepath.Join(t.TempDir(), "concurrency.db")
	db, err := NewDB(dbPath, &logger)
	assert.NoError(t, err)
	defer db.Close()

	ctx := context.Background()

	// Create an item with quantity 1
	item := &models.Item{
		Name:          "Limited Item",
		TotalQuantity: 1,
		IsActive:      true,
	}
	err = db.CreateItem(ctx, item)
	assert.NoError(t, err)

	date := time.Now().AddDate(0, 0, 1)

	const numGoroutines = 10
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	results := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			booking := &models.Booking{
				UserID:   int64(id),
				UserName: "User",
				ItemID:   item.ID,
				Date:     date,
				Status:   models.StatusPending,
			}
			// We use CreateBookingWithLock which has internal locking/checks
			bErr := db.CreateBookingWithLock(ctx, booking)
			results <- bErr
		}(i)
	}

	wg.Wait()
	close(results)

	successCount := 0
	failCount := 0
	for err := range results {
		if err == nil {
			successCount++
		} else {
			failCount++
		}
	}

	// Only 1 booking should succeed because total_quantity is 1
	assert.Equal(t, 1, successCount, "Only one booking should succeed for item with quantity 1")
	assert.Equal(t, numGoroutines-1, failCount, "All other bookings should fail")

	// Verify in DB
	count, err := db.GetBookedCount(ctx, item.ID, date)
	assert.NoError(t, err)
	assert.Equal(t, 1, count)

	// Check if we can find the booking
	bookings, err := db.GetBookingsByDateRange(ctx, date, date)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(bookings))
}
