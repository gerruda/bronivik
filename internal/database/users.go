package database

import (
	"context"
	"fmt"
	"time"

	"bronivik/internal/models"
)

func (db *DB) CreateOrUpdateUser(ctx context.Context, user *models.User) error {
	query := `INSERT INTO users (
				telegram_id, username, first_name, last_name, phone, 
				is_manager, is_blacklisted, language_code, 
				last_activity, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
              ON CONFLICT(telegram_id) DO UPDATE SET
                username = excluded.username,
                first_name = excluded.first_name,
                last_name = excluded.last_name,
                language_code = excluded.language_code,
                last_activity = excluded.last_activity,
                updated_at = excluded.updated_at`
	lastActivity := user.LastActivity
	if lastActivity.IsZero() {
		lastActivity = time.Now()
	}
	now := time.Now()
	_, err := db.ExecContext(ctx, query,
		user.TelegramID,
		user.Username,
		user.FirstName,
		user.LastName,
		user.Phone,
		user.IsManager,
		user.IsBlacklisted,
		user.LanguageCode,
		lastActivity,
		now,
		now,
	)
	if err != nil {
		return fmt.Errorf("failed to create or update user: %w", err)
	}
	return nil
}

func (db *DB) GetUserByTelegramID(ctx context.Context, telegramID int64) (*models.User, error) {
	query := `SELECT id, telegram_id, username, first_name, last_name, 
	                 phone, is_manager, is_blacklisted, language_code, 
					 last_activity, created_at, updated_at 
              FROM users WHERE telegram_id = ?`
	return db.queryUser(ctx, query, telegramID)
}

func (db *DB) GetUserByID(ctx context.Context, id int64) (*models.User, error) {
	query := `SELECT id, telegram_id, username, first_name, last_name, 
	                 phone, is_manager, is_blacklisted, language_code, 
					 last_activity, created_at, updated_at 
              FROM users WHERE id = ?`
	return db.queryUser(ctx, query, id)
}

func (db *DB) queryUser(ctx context.Context, query string, args ...interface{}) (*models.User, error) {
	var user models.User
	err := db.QueryRowContext(ctx, query, args...).Scan(
		&user.ID, &user.TelegramID, &user.Username, &user.FirstName, &user.LastName, &user.Phone,
		&user.IsManager, &user.IsBlacklisted, &user.LanguageCode, &user.LastActivity, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (db *DB) UpdateUserPhone(ctx context.Context, telegramID int64, phone string) error {
	query := `UPDATE users SET phone = ?, updated_at = ? WHERE telegram_id = ?`
	_, err := db.ExecContext(ctx, query, phone, time.Now(), telegramID)
	return err
}

func (db *DB) UpdateUserActivity(ctx context.Context, telegramID int64) error {
	query := `UPDATE users SET last_activity = ?, updated_at = ? WHERE telegram_id = ?`
	now := time.Now()
	_, err := db.ExecContext(ctx, query, now, now, telegramID)
	return err
}

func (db *DB) GetAllUsers(ctx context.Context) ([]*models.User, error) {
	query := `SELECT id, telegram_id, username, first_name, last_name, 
		phone, is_manager, is_blacklisted, language_code, 
		last_activity, created_at, updated_at
              FROM users ORDER BY last_activity DESC`
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get all users: %w", err)
	}
	defer rows.Close()

	var users []*models.User
	for rows.Next() {
		u := &models.User{}
		err := rows.Scan(
			&u.ID, &u.TelegramID, &u.Username, &u.FirstName, &u.LastName, &u.Phone,
			&u.IsManager, &u.IsBlacklisted, &u.LanguageCode, &u.LastActivity, &u.CreatedAt, &u.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}
		users = append(users, u)
	}
	return users, nil
}

func (db *DB) GetUsersByManagerStatus(ctx context.Context, isManager bool) ([]*models.User, error) {
	query := `SELECT id, telegram_id, username, first_name, last_name, 
		phone, is_manager, is_blacklisted, language_code, 
		last_activity, created_at, updated_at
              FROM users WHERE is_manager = ? ORDER BY last_activity DESC`
	rows, err := db.QueryContext(ctx, query, isManager)
	if err != nil {
		return nil, fmt.Errorf("failed to get users by manager status: %w", err)
	}
	defer rows.Close()

	var users []*models.User
	for rows.Next() {
		u := &models.User{}
		err := rows.Scan(
			&u.ID, &u.TelegramID, &u.Username, &u.FirstName, &u.LastName, &u.Phone,
			&u.IsManager, &u.IsBlacklisted, &u.LanguageCode, &u.LastActivity, &u.CreatedAt, &u.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}
		users = append(users, u)
	}
	return users, nil
}

func (db *DB) GetActiveUsers(ctx context.Context, days int) ([]*models.User, error) {
	since := time.Now().AddDate(0, 0, -days)
	query := `SELECT id, telegram_id, username, first_name, last_name, 
		phone, is_manager, is_blacklisted, language_code, 
		last_activity, created_at, updated_at
              FROM users WHERE last_activity >= ? ORDER BY last_activity DESC`
	rows, err := db.QueryContext(ctx, query, since)
	if err != nil {
		return nil, fmt.Errorf("failed to get active users: %w", err)
	}
	defer rows.Close()

	var users []*models.User
	for rows.Next() {
		u := &models.User{}
		err := rows.Scan(
			&u.ID, &u.TelegramID, &u.Username, &u.FirstName, &u.LastName, &u.Phone,
			&u.IsManager, &u.IsBlacklisted, &u.LanguageCode, &u.LastActivity, &u.CreatedAt, &u.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}
		users = append(users, u)
	}
	return users, nil
}
