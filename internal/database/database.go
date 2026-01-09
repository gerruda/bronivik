package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"bronivik/internal/models"

	_ "github.com/mattn/go-sqlite3" // sqlite3 driver
	"github.com/rs/zerolog"
)

// DB represents the database connection and its cache.
type DB struct {
	*sql.DB
	itemsCache map[int64]models.Item
	cacheTime  time.Time
	mu         sync.RWMutex
	logger     *zerolog.Logger
}

var (
	ErrConcurrentModification = errors.New("concurrent modification")
	ErrNotAvailable           = errors.New("not available")
	ErrPastDate               = errors.New("cannot book in the past")
	ErrDateTooFar             = errors.New("date is too far in the future")
)

// NewDB initializes a new database connection and creates tables if they don't exist.
func NewDB(path string, logger *zerolog.Logger) (*DB, error) {
	// Создаем директорию для БД, если её нет
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Добавляем параметры для SQLite: WAL mode, busy timeout
	dsn := path + "?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Настройка пула соединений
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	// Проверяем соединение
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	instance := &DB{
		DB:         db,
		itemsCache: make(map[int64]models.Item),
		logger:     logger,
	}

	// Создаем таблицы
	if err := instance.createTables(); err != nil {
		return nil, fmt.Errorf("failed to create tables: %v", err)
	}

	// Load items into cache
	if err := instance.LoadItems(context.Background()); err != nil {
		logger.Error().Err(err).Msg("Failed to load items into cache")
		// We don't return error here to allow the app to start even if items are missing
	}

	logger.Info().Str("path", path).Msg("Database initialized")
	return instance, nil
}

func (db *DB) createTables() error {
	queries := []string{
		// Таблица предметов (аппаратов)
		`CREATE TABLE IF NOT EXISTS items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE NOT NULL,
			description TEXT,
			total_quantity INTEGER NOT NULL DEFAULT 1,
			sort_order INTEGER NOT NULL DEFAULT 0,
			is_active BOOLEAN NOT NULL DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		// Таблица пользователей
		`CREATE TABLE IF NOT EXISTS users (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            telegram_id INTEGER UNIQUE NOT NULL,
            username TEXT,
            first_name TEXT NOT NULL,
            last_name TEXT,
            phone TEXT,
            is_manager BOOLEAN NOT NULL DEFAULT 0,
            is_blacklisted BOOLEAN NOT NULL DEFAULT 0,
            language_code TEXT,
            last_activity DATETIME NOT NULL,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
        )`,
		// Таблица бронирований
		`CREATE TABLE IF NOT EXISTS bookings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			user_name TEXT NOT NULL,
			user_nickname TEXT,
			phone TEXT NOT NULL,
			item_id INTEGER NOT NULL,
			item_name TEXT NOT NULL,
			date DATETIME NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			comment TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			version INTEGER NOT NULL DEFAULT 1,
			FOREIGN KEY(item_id) REFERENCES items(id),
			FOREIGN KEY(user_id) REFERENCES users(telegram_id)
		)`,

		// Индексы для пользователей
		`CREATE INDEX IF NOT EXISTS idx_users_telegram_id ON users(telegram_id)`,
		`CREATE INDEX IF NOT EXISTS idx_users_is_manager ON users(is_manager)`,
		`CREATE INDEX IF NOT EXISTS idx_users_is_blacklisted ON users(is_blacklisted)`,

		// Индексы для items
		`CREATE INDEX IF NOT EXISTS idx_items_sort ON items(sort_order, id)`,

		// Уникальный индекс для предотвращения двойного бронирования (если количество = 1)
		// Примечание: это работает только если TotalQuantity всегда 1.
		// Если TotalQuantity > 1, логика должна быть сложнее (в коде через транзакции).
		// Но для базовой защиты добавим индекс по (item_id, date, status)
		`CREATE INDEX IF NOT EXISTS idx_bookings_item_date_status ON bookings(item_id, date, status)`,

		// Очередь синхронизации в Sheets
		`CREATE TABLE IF NOT EXISTS sync_queue (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_type TEXT NOT NULL,
			booking_id INTEGER NOT NULL,
			payload TEXT,
			status TEXT DEFAULT 'pending',
			retry_count INTEGER DEFAULT 0,
			last_error TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			processed_at DATETIME,
			next_retry_at DATETIME
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sync_queue_status ON sync_queue(status)`,
		`CREATE INDEX IF NOT EXISTS idx_sync_queue_next_retry ON sync_queue(next_retry_at)`,

		// Существующие индексы для бронирований
		`CREATE INDEX IF NOT EXISTS idx_bookings_date ON bookings(date)`,
		`CREATE INDEX IF NOT EXISTS idx_bookings_status ON bookings(status)`,
		`CREATE INDEX IF NOT EXISTS idx_bookings_item_id ON bookings(item_id)`,
		`CREATE INDEX IF NOT EXISTS idx_bookings_user_id ON bookings(user_id)`,
	}

	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return fmt.Errorf("error executing query %s: %v", query, err)
		}
	}

	if err := db.ensureBookingVersionColumn(); err != nil {
		return err
	}
	return nil
}

func (db *DB) ensureBookingVersionColumn() error {
	_, err := db.Exec(`ALTER TABLE bookings ADD COLUMN version INTEGER NOT NULL DEFAULT 1`)
	if err != nil {
		// Ignore duplicate column error for SQLite
		if strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return nil
		}
		return fmt.Errorf("failed to add version column: %w", err)
	}
	return nil
}

func (db *DB) Close() error {
	return db.DB.Close()
}
