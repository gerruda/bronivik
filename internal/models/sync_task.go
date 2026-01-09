package models

import "time"

// SyncTask represents a queued synchronization job for Sheets.
type SyncTask struct {
	ID         int64      `json:"id"`
	TaskType   string     `json:"task_type"`
	BookingID  int64      `json:"booking_id"`
	Payload    string     `json:"payload"`
	Status     string     `json:"status"`
	RetryCount int        `json:"retry_count"`
	LastError  *string    `json:"last_error"`
	CreatedAt  time.Time  `json:"created_at"`
	ProcessedAt *time.Time `json:"processed_at"`
	NextRetryAt *time.Time `json:"next_retry_at"`
}
