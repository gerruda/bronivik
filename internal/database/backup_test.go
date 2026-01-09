package database

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bronivik/internal/config"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackupService(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "backup_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "source.db")
	storagePath := filepath.Join(tempDir, "backups")

	// Create source DB
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	_, err = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY)")
	require.NoError(t, err)
	db.Close()

	cfg := config.BackupConfig{
		Enabled:       true,
		StoragePath:   storagePath,
		RetentionDays: 1,
	}
	logger := zerolog.Nop()
	s := NewBackupService(dbPath, cfg, &logger)

	t.Run("PerformBackup", func(t *testing.T) {
		err := s.PerformBackup()
		assert.NoError(t, err)

		files, err := os.ReadDir(storagePath)
		assert.NoError(t, err)
		assert.Len(t, files, 1)
	})

	t.Run("CleanupOldBackups", func(t *testing.T) {
		// Create an old file
		oldFile := filepath.Join(storagePath, "backup_old.db")
		err := os.WriteFile(oldFile, []byte("old"), 0o644)
		require.NoError(t, err)

		// Set mod time to 2 days ago
		oldTime := time.Now().AddDate(0, 0, -2)
		err = os.Chtimes(oldFile, oldTime, oldTime)
		require.NoError(t, err)

		s.CleanupOldBackups()

		files, err := os.ReadDir(storagePath)
		assert.NoError(t, err)
		// The new backup from previous test should remain, the old one should be gone
		assert.Len(t, files, 1)
		assert.NotEqual(t, "backup_old.db", files[0].Name())
	})
}

func TestBackupService_Disabled(_ *testing.T) {
	logger := zerolog.Nop()
	s := NewBackupService("any", config.BackupConfig{Enabled: false}, &logger)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Stop immediately
	s.Start(ctx)
	// Should just return
}
