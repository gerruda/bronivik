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

func TestProcessTaskFail(t *testing.T) {
	db := newTestDB(t)
	sheets := &fakeSheets{err: errors.New("fatal")}
	worker := NewSheetsWorker(db, sheets, nil, RetryPolicy{MaxRetries: 1}, nil)

	booking := &models.Booking{ID: 3, UserID: 1, UserName: "tester", Phone: "+100", ItemID: 10, ItemName: "camera", Date: time.Now(), Status: "pending", CreatedAt: time.Now(), UpdatedAt: time.Now()}

	ctx := context.Background()
	worker.EnqueueTask(ctx, TaskUpsert, booking.ID, booking, "")
	task, _ := worker.tryLocalQueue()
	worker.processTask(ctx, &task)

	status, _, _ := loadTaskStatus(t, db, task.ID)
	if status != "failed" {
		t.Fatalf("expected status=failed, got %s", status)
	}
}

func TestSheetsWorker_EnqueueSyncSchedule(t *testing.T) {
	db := newTestDB(t)
	sheets := &fakeSheets{}
	worker := NewSheetsWorker(db, sheets, nil, RetryPolicy{MaxRetries: 3}, nil)

	ctx := context.Background()
	start := time.Now()
	end := start.AddDate(0, 0, 7)

	err := worker.EnqueueSyncSchedule(ctx, start, end)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	tasks, _ := db.GetPendingSyncTasks(ctx, 10)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].TaskType != TaskSyncSchedule {
		t.Fatalf("expected TaskSyncSchedule, got %s", tasks[0].TaskType)
	}
}

func TestSheetsWorker_HandleSheetTask(t *testing.T) {
	db := newTestDB(t)
	sheets := &fakeSheets{}
	worker := NewSheetsWorker(db, sheets, nil, RetryPolicy{MaxRetries: 3}, nil)

	ctx := context.Background()

	t.Run("Upsert", func(t *testing.T) {
		booking := &models.Booking{ID: 1, ItemName: "Test"}
		err := worker.handleSheetTask(ctx, TaskUpsert, sheetTaskPayload{Booking: booking})
		if err != nil {
			t.Fatalf("handle: %v", err)
		}
		if sheets.upsertCalls != 1 {
			t.Fatalf("expected 1 upsert call, got %d", sheets.upsertCalls)
		}
	})

	t.Run("Delete", func(t *testing.T) {
		err := worker.handleSheetTask(ctx, TaskDelete, sheetTaskPayload{BookingID: 123})
		if err != nil {
			t.Fatalf("handle: %v", err)
		}
		if sheets.deleteCalls != 1 {
			t.Fatalf("expected 1 delete call, got %d", sheets.deleteCalls)
		}
	})

	t.Run("UpdateStatus", func(t *testing.T) {
		err := worker.handleSheetTask(ctx, TaskUpdateStatus, sheetTaskPayload{BookingID: 123, Status: "confirmed"})
		if err != nil {
			t.Fatalf("handle: %v", err)
		}
		if sheets.statusCalls != 1 {
			t.Fatalf("expected 1 status call, got %d", sheets.statusCalls)
		}
	})
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

func TestSheetsWorker_EnqueueTask(t *testing.T) {
	db := newTestDB(t)
	sheets := &fakeSheets{}
	worker := NewSheetsWorker(db, sheets, nil, RetryPolicy{}, nil)

	ctx := context.Background()
	booking := &models.Booking{ID: 1, UserName: "test"}

	t.Run("ValidTask", func(t *testing.T) {
		err := worker.EnqueueTask(ctx, TaskUpsert, 1, booking, "")
		if err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	})

	t.Run("InvalidTaskType", func(t *testing.T) {
		err := worker.EnqueueTask(ctx, "", 1, booking, "")
		if err == nil {
			t.Fatalf("expected error for empty task type")
		}
	})

	t.Run("InvalidBookingID", func(t *testing.T) {
		err := worker.EnqueueTask(ctx, TaskUpsert, 0, nil, "")
		if err == nil {
			t.Fatalf("expected error for missing booking id")
		}
	})
}

func TestSheetsWorker_DecodePayload(t *testing.T) {
	worker := NewSheetsWorker(nil, nil, nil, RetryPolicy{}, nil)

	t.Run("ValidPayload", func(t *testing.T) {
		payload := `{"booking_id":123,"status":"confirmed"}`
		decoded, err := worker.decodePayload(payload)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if decoded.BookingID != 123 || decoded.Status != "confirmed" {
			t.Fatalf("unexpected decoded payload: %+v", decoded)
		}
	})

	t.Run("InvalidPayload", func(t *testing.T) {
		payload := `invalid json`
		_, err := worker.decodePayload(payload)
		if err == nil {
			t.Fatalf("expected error for invalid json")
		}
	})
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
