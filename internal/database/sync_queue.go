package database

import (
	"context"
	"fmt"
	"time"

	"bronivik/internal/models"
)

func (db *DB) CreateSyncTask(ctx context.Context, task *models.SyncTask) error {
	query := `INSERT INTO sync_queue (task_type, booking_id, payload, status, retry_count, last_error, created_at, next_retry_at)
              VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	now := time.Now()
	result, err := db.ExecContext(ctx, query,
		task.TaskType,
		task.BookingID,
		task.Payload,
		task.Status,
		task.RetryCount,
		task.LastError,
		now,
		task.NextRetryAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create sync task: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get last insert id: %w", err)
	}
	task.ID = id
	task.CreatedAt = now

	return nil
}

func (db *DB) GetPendingSyncTasks(ctx context.Context, limit int) ([]models.SyncTask, error) {
	query := `SELECT id, task_type, booking_id, payload, status, retry_count, last_error, created_at, processed_at, next_retry_at 
              FROM sync_queue 
              WHERE status IN ('pending', 'retry') AND (next_retry_at IS NULL OR next_retry_at <= ?) 
              ORDER BY created_at ASC LIMIT ?`
	rows, err := db.QueryContext(ctx, query, time.Now(), limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get pending sync tasks: %w", err)
	}
	defer rows.Close()

	var tasks []models.SyncTask
	for rows.Next() {
		var t models.SyncTask
		err := rows.Scan(
			&t.ID, &t.TaskType, &t.BookingID, &t.Payload, &t.Status, &t.RetryCount, &t.LastError, &t.CreatedAt, &t.ProcessedAt, &t.NextRetryAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan sync task: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}

func (db *DB) UpdateSyncTaskStatus(ctx context.Context, id int64, status, errMsg string, nextRetryAt *time.Time) error {
	var query string
	var args []interface{}
	now := time.Now()

	switch status {
	case "retry":
		query = `UPDATE sync_queue SET status = ?, last_error = ?, next_retry_at = ?, retry_count = retry_count + 1 WHERE id = ?`
		args = []interface{}{status, errMsg, nextRetryAt, id}
	case "completed", "failed":
		query = `UPDATE sync_queue SET status = ?, last_error = ?, next_retry_at = ?, processed_at = ? WHERE id = ?`
		args = []interface{}{status, errMsg, nextRetryAt, &now, id}
	default:
		query = `UPDATE sync_queue SET status = ?, last_error = ?, next_retry_at = ? WHERE id = ?`
		args = []interface{}{status, errMsg, nextRetryAt, id}
	}

	_, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update sync task status: %w", err)
	}
	return nil
}

func (db *DB) GetFailedSyncTasks(ctx context.Context) ([]models.SyncTask, error) {
	query := `SELECT id, task_type, booking_id, payload, status, retry_count, last_error, created_at, processed_at, next_retry_at 
              FROM sync_queue WHERE status = 'failed' ORDER BY created_at DESC`
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get failed sync tasks: %w", err)
	}
	defer rows.Close()

	var tasks []models.SyncTask
	for rows.Next() {
		var t models.SyncTask
		err := rows.Scan(
			&t.ID, &t.TaskType, &t.BookingID, &t.Payload, &t.Status, &t.RetryCount, &t.LastError, &t.CreatedAt, &t.ProcessedAt, &t.NextRetryAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan sync task: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}
