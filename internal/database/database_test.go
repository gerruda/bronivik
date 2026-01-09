package database

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDB_DirectoryCreation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "db_test_dir")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "nested", "dir", "test.db")
	logger := zerolog.Nop()

	db, err := NewDB(dbPath, &logger)
	require.NoError(t, err)
	defer db.Close()

	assert.FileExists(t, dbPath)
}

func TestEnsureBookingVersionColumn(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Calling it twice should not fail (testing the "duplicate column" suppression)
	err := db.ensureBookingVersionColumn()
	require.NoError(t, err)
}

func TestDB_Ping(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	err := db.PingContext(context.Background())
	assert.NoError(t, err)
}
