package database

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"bronivik/internal/config"

	_ "github.com/mattn/go-sqlite3" // sqlite3 driver
	"github.com/rs/zerolog"
)

type BackupService struct {
	dbPath string
	config config.BackupConfig
	logger *zerolog.Logger
}

func NewBackupService(dbPath string, cfg config.BackupConfig, logger *zerolog.Logger) *BackupService {
	return &BackupService{
		dbPath: dbPath,
		config: cfg,
		logger: logger,
	}
}

func (s *BackupService) Start(ctx context.Context) {
	if !s.config.Enabled {
		s.logger.Info().Msg("Backup service is disabled")
		return
	}

	s.logger.Info().Str("schedule", s.config.Schedule).Msg("Backup service started")

	interval := 24 * time.Hour
	if s.config.Schedule != "" {
		if d, err := time.ParseDuration(s.config.Schedule); err == nil {
			interval = d
		} else {
			s.logger.Warn().Err(err).Str("schedule", s.config.Schedule).Msg("Failed to parse backup schedule, using default 24h")
		}
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run first backup immediately
	if err := s.PerformBackup(); err != nil {
		s.logger.Error().Err(err).Msg("Initial backup failed")
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.PerformBackup(); err != nil {
				s.logger.Error().Err(err).Msg("Scheduled backup failed")
			}
			s.CleanupOldBackups()
		}
	}
}

func (s *BackupService) PerformBackup() error {
	if _, err := os.Stat(s.config.StoragePath); os.IsNotExist(err) {
		if err := os.MkdirAll(s.config.StoragePath, 0o755); err != nil {
			return fmt.Errorf("failed to create backup directory: %w", err)
		}
	}

	timestamp := time.Now().Format("20060102_150405")
	backupFileName := fmt.Sprintf("backup_%s.db", timestamp)
	backupPath := filepath.Join(s.config.StoragePath, backupFileName)

	s.logger.Info().Str("path", backupPath).Msg("Performing database backup using VACUUM INTO")

	db, err := sql.Open("sqlite3", s.dbPath)
	if err != nil {
		return fmt.Errorf("failed to open source database: %w", err)
	}
	defer db.Close()

	// Use VACUUM INTO for a safe online backup
	_, err = db.Exec(fmt.Sprintf("VACUUM INTO '%s'", backupPath))
	if err != nil {
		s.logger.Warn().Err(err).Msg("VACUUM INTO failed, falling back to file copy")
		return s.performBackupFallback(backupPath)
	}

	s.logger.Info().Msg("Backup completed successfully")
	return nil
}

func (s *BackupService) performBackupFallback(backupPath string) error {
	source, err := os.Open(s.dbPath)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(backupPath)
	if err != nil {
		return err
	}
	defer destination.Close()

	// Note: io.Copy is not atomic for SQLite and might result in a corrupted backup if writes occur
	_, err = io.Copy(destination, source)
	if err != nil {
		return err
	}

	s.logger.Info().Msg("Fallback backup completed successfully")
	return nil
}

func (s *BackupService) CleanupOldBackups() {
	if s.config.RetentionDays <= 0 {
		return
	}

	files, err := os.ReadDir(s.config.StoragePath)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to read backup directory for cleanup")
		return
	}

	cutoff := time.Now().AddDate(0, 0, -s.config.RetentionDays)

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		info, err := file.Info()
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoff) {
			s.logger.Info().Str("file", file.Name()).Msg("Deleting old backup")
			os.Remove(filepath.Join(s.config.StoragePath, file.Name()))
		}
	}
}
