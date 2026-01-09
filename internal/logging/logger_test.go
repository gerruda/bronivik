package logging

import (
	"os"
	"path/filepath"
	"testing"

	"bronivik/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLogger(t *testing.T) {
	appCfg := config.AppConfig{
		Name:        "test-app",
		Environment: "test",
		Version:     "1.0.0",
	}

	t.Run("DefaultStdout", func(t *testing.T) {
		cfg := config.LoggingConfig{Level: "info", Output: "stdout"}
		logger, closer, err := New(cfg, appCfg)
		require.NoError(t, err)
		assert.NotNil(t, logger)
		assert.Nil(t, closer)
	})

	t.Run("Stderr", func(t *testing.T) {
		cfg := config.LoggingConfig{Level: "debug", Output: "stderr"}
		logger, closer, err := New(cfg, appCfg)
		require.NoError(t, err)
		assert.NotNil(t, logger)
		assert.Nil(t, closer)
	})

	t.Run("Console", func(t *testing.T) {
		cfg := config.LoggingConfig{Level: "warn", Output: "stdout", Format: "console"}
		logger, closer, err := New(cfg, appCfg)
		require.NoError(t, err)
		assert.NotNil(t, logger)
		assert.Nil(t, closer)
	})

	t.Run("File", func(t *testing.T) {
		tmpDir := t.TempDir()
		logPath := filepath.Join(tmpDir, "test.log")
		cfg := config.LoggingConfig{Level: "error", Output: "file", FilePath: logPath}
		logger, closer, err := New(cfg, appCfg)
		require.NoError(t, err)
		assert.NotNil(t, logger)
		assert.NotNil(t, closer)
		closer.Close()

		_, err = os.Stat(logPath)
		assert.NoError(t, err)
	})

	t.Run("FileMissingPath", func(t *testing.T) {
		cfg := config.LoggingConfig{Output: "file", FilePath: ""}
		_, _, err := New(cfg, appCfg)
		assert.Error(t, err)
	})

	t.Run("InvalidLevel", func(t *testing.T) {
		cfg := config.LoggingConfig{Level: "invalid"}
		logger, _, err := New(cfg, appCfg)
		require.NoError(t, err) // Should default to info
		assert.NotNil(t, logger)
	})
}
