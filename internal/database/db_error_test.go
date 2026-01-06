package database

import (
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bronivik/internal/config"
	"bronivik/internal/models"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestDB_ErrorPaths(t *testing.T) {
	logger := zerolog.New(io.Discard)
	db, err := NewDB(":memory:", &logger)
	assert.NoError(t, err)
	db.Close() // Close the DB to trigger errors

	ctx := context.Background()

	t.Run("CheckAvailability_Error", func(t *testing.T) {
		_, err := db.CheckAvailability(ctx, 1, time.Now())
		assert.Error(t, err)
	})

	t.Run("CreateBooking_Error", func(t *testing.T) {
		err := db.CreateBooking(ctx, &models.Booking{})
		assert.Error(t, err)
	})

	t.Run("GetBookingsByDateRange_Error", func(t *testing.T) {
		_, err := db.GetBookingsByDateRange(ctx, time.Now(), time.Now())
		assert.Error(t, err)
	})

	t.Run("SyncItems_Error", func(t *testing.T) {
		err := db.SyncItems(ctx, []models.Item{})
		assert.Error(t, err)
	})

	t.Run("UpdateUserActivity_Error", func(t *testing.T) {
		err := db.UpdateUserActivity(ctx, 123)
		assert.Error(t, err)
	})

	t.Run("CreateSyncTask_Error", func(t *testing.T) {
		err := db.CreateSyncTask(ctx, &models.SyncTask{})
		assert.Error(t, err)
	})

	t.Run("ItemCacheMiss", func(t *testing.T) {
		// New DB for clean state
		logger := zerolog.New(io.Discard)
		db2, _ := NewDB(":memory:", &logger)
		defer db2.Close()

		// Insert manually to bypass cache update from CreateItem
		query := `INSERT INTO items (
			id, name, description, total_quantity, sort_order, 
			is_active, created_at, updated_at
		) VALUES (99, 'Test Item', '', 10, 1, 1, ?, ?)`
		_, err := db2.ExecContext(ctx, query, time.Now(), time.Now())
		assert.NoError(t, err)

		// This should hit DB and fill cache
		item, err := db2.GetItemByID(ctx, 99)
		assert.NoError(t, err)
		if assert.NotNil(t, item) {
			assert.Equal(t, int64(99), item.ID)
		}

		// Hit cache now
		item2, err := db2.GetItemByID(ctx, 99)
		assert.NoError(t, err)
		if assert.NotNil(t, item2) {
			assert.Equal(t, item.ID, item2.ID)
		}

		// Get by name (not in cache yet)
		item3, err := db2.GetItemByName(ctx, "Test Item")
		assert.NoError(t, err)
		if assert.NotNil(t, item3) {
			assert.Equal(t, "Test Item", item3.Name)
		}
	})
}

func TestBackupService_Extended(t *testing.T) {
	logger := zerolog.New(io.Discard)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "source.db")
	storagePath := filepath.Join(tempDir, "backups")

	// Create source DB
	db, err := sql.Open("sqlite3", dbPath)
	assert.NoError(t, err)
	_, err = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY)")
	assert.NoError(t, err)
	db.Close()

	cfg := config.BackupConfig{
		Enabled:     true,
		StoragePath: storagePath,
	}
	s := NewBackupService(dbPath, cfg, &logger)

	t.Run("Fallback", func(t *testing.T) {
		backupPath := filepath.Join(storagePath, "fallback_test.db")
		err = os.MkdirAll(storagePath, 0o755)
		assert.NoError(t, err)

		err = s.performBackupFallback(backupPath)
		assert.NoError(t, err)

		_, err = os.Stat(backupPath)
		assert.NoError(t, err)
	})

	t.Run("Loop", func(t *testing.T) {
		cfgLoop := cfg
		cfgLoop.Schedule = "10ms"
		cfgLoop.StoragePath = filepath.Join(tempDir, "backups_loop")
		sLoop := NewBackupService(dbPath, cfgLoop, &logger)

		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()

		sLoop.Start(ctx)

		files, _ := os.ReadDir(cfgLoop.StoragePath)
		assert.True(t, len(files) > 0)
	})
}

func TestBackupService_RecursiveError(t *testing.T) {
	// Use a path that is actually a file to make MkdirAll fail
	tmpFile, _ := os.CreateTemp("", "notadir")
	defer os.Remove(tmpFile.Name())

	dbPath := ":memory:"
	// StoragePath pointing to a file will make MkdirAll fail
	cfg := config.BackupConfig{Enabled: true, StoragePath: tmpFile.Name() + "/subdir"}
	logger := zerolog.New(io.Discard)
	bs := NewBackupService(dbPath, cfg, &logger)

	err := bs.PerformBackup()
	assert.Error(t, err)
}

func TestNewDB_Error(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "db_err")
	defer os.RemoveAll(tmpDir)

	logger := zerolog.New(io.Discard)
	_, err := NewDB(tmpDir, &logger)
	assert.Error(t, err)
}

func TestUpdateBookingStatus_Error(t *testing.T) {
	logger := zerolog.New(io.Discard)
	db, _ := NewDB(":memory:", &logger)
	db.Close()
	err := db.UpdateBookingStatus(context.Background(), 1, "confirmed")
	assert.Error(t, err)
}

func TestGetDailyBookings_Error(t *testing.T) {
	logger := zerolog.New(io.Discard)
	db, _ := NewDB(":memory:", &logger)
	db.Close()
	_, err := db.GetDailyBookings(context.Background(), time.Now(), time.Now())
	assert.Error(t, err)
}

func TestGetAvailabilityForPeriod_Error(t *testing.T) {
	logger := zerolog.New(io.Discard)
	db, _ := NewDB(":memory:", &logger)
	db.Close()
	_, err := db.GetAvailabilityForPeriod(context.Background(), 1, time.Now(), 1)
	assert.Error(t, err)
}

func TestCreateBookingWithLock_Error(t *testing.T) {
	logger := zerolog.New(io.Discard)
	db, _ := NewDB(":memory:", &logger)
	db.Close()
	err := db.CreateBookingWithLock(context.Background(), &models.Booking{})
	assert.Error(t, err)
}
