package worker

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bronivik/internal/database"
	"bronivik/internal/models"

	"github.com/rs/zerolog"
)

func TestProcessTaskSuccess(t *testing.T) {
	db := newTestDB(t)
	sheets := &fakeSheets{}
	worker := NewSheetsWorker(db, sheets, nil, RetryPolicy{}, nil)

	booking := &models.Booking{
		ID:        1,
		UserID:    1,
		UserName:  "tester",
		Phone:     "+100",
		ItemID:    10,
		ItemName:  "camera",
		Date:      time.Now(),
		Status:    "pending",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	ctx := context.Background()
	if err := worker.EnqueueTask(ctx, TaskUpsert, booking.ID, booking, ""); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	task, ok := worker.tryLocalQueue()
	if !ok {
		t.Fatalf("expected task in local queue")
	}
	worker.processTask(ctx, &task)

	status, retryCount, nextRetry := loadTaskStatus(t, db, task.ID)
	if status != "completed" {
		t.Fatalf("expected status=completed, got %s", status)
	}
	if retryCount != 0 {
		t.Fatalf("expected retry_count=0, got %d", retryCount)
	}
	if nextRetry.Valid {
		t.Fatalf("expected next_retry_at NULL on success")
	}
	if sheets.upsertCalls != 1 {
		t.Fatalf("expected upsert call, got %d", sheets.upsertCalls)
	}
}

func TestProcessTaskRetry(t *testing.T) {
	db := newTestDB(t)
	sheets := &fakeSheets{err: errors.New("boom")}
	worker := NewSheetsWorker(db, sheets, nil, RetryPolicy{MaxRetries: 3, InitialDelay: time.Second}, nil)

	booking := &models.Booking{ID: 2, UserID: 1, UserName: "tester", Phone: "+100", ItemID: 10, ItemName: "camera", Date: time.Now(), Status: "pending", CreatedAt: time.Now(), UpdatedAt: time.Now()}

	ctx := context.Background()
	if err := worker.EnqueueTask(ctx, TaskUpsert, booking.ID, booking, ""); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	task, ok := worker.tryLocalQueue()
	if !ok {
		t.Fatalf("expected task in local queue")
	}
	worker.processTask(ctx, &task)

	status, retryCount, nextRetry := loadTaskStatus(t, db, task.ID)
	if status != "retry" {
		t.Fatalf("expected status=retry, got %s", status)
	}
	if retryCount != 1 {
		t.Fatalf("expected retry_count=1, got %d", retryCount)
	}
	if !nextRetry.Valid || nextRetry.Time.Before(time.Now()) {
		t.Fatalf("expected next_retry_at in future, got %v", nextRetry)
	}
}

func TestRetryPolicyNextDelay(t *testing.T) {
	policy := RetryPolicy{InitialDelay: time.Second, BackoffFactor: 2, MaxDelay: 5 * time.Second}
	d1 := policy.NextDelay(1)
	d2 := policy.NextDelay(2)
	d3 := policy.NextDelay(5)

	if d1 != time.Second {
		t.Fatalf("attempt1 expected 1s, got %s", d1)
	}
	if d2 != 2*time.Second {
		t.Fatalf("attempt2 expected 2s, got %s", d2)
	}
	if d3 != 5*time.Second {
		t.Fatalf("attempt5 expected capped 5s, got %s", d3)
	}
}

// Helpers

type fakeSheets struct {
	err         error
	upsertCalls int
	deleteCalls int
	statusCalls int
}

func (f *fakeSheets) UpsertBooking(ctx context.Context, b *models.Booking) error {
	f.upsertCalls++
	return f.err
}

func (f *fakeSheets) DeleteBookingRow(ctx context.Context, id int64) error {
	f.deleteCalls++
	return f.err
}

func (f *fakeSheets) UpdateBookingStatus(ctx context.Context, id int64, status string) error {
	f.statusCalls++
	return f.err
}

func (f *fakeSheets) UpdateScheduleSheet(ctx context.Context, startDate, endDate time.Time, dailyBookings map[string][]models.Booking, items []models.Item) error {
	return f.err
}

func newTestDB(t *testing.T) *database.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "worker.db")
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
	db, err := database.NewDB(path, &logger)
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func loadTaskStatus(t *testing.T, db *database.DB, id int64) (status string, retryCount int, nextRetry sql.NullTime) {
	t.Helper()
	row := db.QueryRowContext(context.Background(), `SELECT status, retry_count, next_retry_at FROM sync_queue WHERE id = ?`, id)
	if err := row.Scan(&status, &retryCount, &nextRetry); err != nil {
		t.Fatalf("scan task: %v", err)
	}
	return status, retryCount, nextRetry
}
