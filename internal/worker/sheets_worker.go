package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"bronivik/internal/database"
	"bronivik/internal/models"
	"github.com/redis/go-redis/v9"
)

const (
	TaskUpsert       = "upsert"
	TaskDelete       = "delete"
	TaskUpdateStatus = "update_status"
)

// SheetTask describes a unit of work for Sheets.
type SheetTask struct {
	Type      string
	BookingID int64
	Booking   *models.Booking
	Status    string
	CreatedAt time.Time
}

// sheetTaskPayload is persisted in SyncTask.Payload as JSON.
type sheetTaskPayload struct {
	BookingID int64           `json:"booking_id"`
	Booking   *models.Booking `json:"booking,omitempty"`
	Status    string          `json:"status,omitempty"`
}

// SheetsWorker consumes sync_queue tasks and applies them to Google Sheets.
type SheetsClient interface {
	UpsertBooking(*models.Booking) error
	DeleteBookingRow(int64) error
	UpdateBookingStatus(int64, string) error
}

type SheetsWorker struct {
	db            *database.DB
	sheets        SheetsClient
	redis         *redis.Client
	retryPolicy   RetryPolicy
	queue         chan models.SyncTask
	redisQueueKey string
	deadLetterKey string
	pollInterval  time.Duration
	batchSize     int
	logger        *log.Logger
}

// NewSheetsWorker builds a worker with sane defaults.
func NewSheetsWorker(db *database.DB, sheets SheetsClient, redisClient *redis.Client, retry RetryPolicy, logger *log.Logger) *SheetsWorker {
	if retry.MaxRetries == 0 {
		retry.MaxRetries = 5
	}
	if retry.InitialDelay == 0 {
		retry.InitialDelay = 2 * time.Second
	}
	if retry.MaxDelay == 0 {
		retry.MaxDelay = 1 * time.Minute
	}
	if retry.BackoffFactor == 0 {
		retry.BackoffFactor = 2
	}
	if logger == nil {
		logger = log.Default()
	}

	return &SheetsWorker{
		db:            db,
		sheets:        sheets,
		redis:         redisClient,
		retryPolicy:   retry,
		queue:         make(chan models.SyncTask, 128),
		redisQueueKey: "sheets:queue",
		deadLetterKey: "sheets:deadletter",
		pollInterval:  2 * time.Second,
		batchSize:     20,
		logger:        logger,
	}
}

// EnqueueTask persists task to DB and schedules it via redis or in-memory queue.
func (w *SheetsWorker) EnqueueTask(ctx context.Context, task SheetTask) error {
	if task.Type == "" {
		return errors.New("task type is required")
	}
	if task.BookingID == 0 && (task.Booking == nil || task.Booking.ID == 0) {
		return errors.New("booking id is required")
	}

	payload := sheetTaskPayload{
		BookingID: task.BookingID,
		Booking:   task.Booking,
		Status:    task.Status,
	}
	if payload.BookingID == 0 && task.Booking != nil {
		payload.BookingID = task.Booking.ID
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode payload: %w", err)
	}

	syncTask := models.SyncTask{
		TaskType:  task.Type,
		BookingID: payload.BookingID,
		Payload:   string(payloadBytes),
		Status:    "pending",
		CreatedAt: time.Now(),
	}

	if err := w.db.CreateSyncTask(ctx, &syncTask); err != nil {
		return fmt.Errorf("persist sync task: %w", err)
	}

	// Try redis first for durability.
	if w.redis != nil {
		if err := w.pushRedis(ctx, syncTask); err != nil {
			w.logger.Printf("sheets_worker: redis push failed, fallback to memory queue: %v", err)
		} else {
			return nil
		}
	}

	// Fallback to in-memory queue if redis missing or failed.
	select {
	case w.queue <- syncTask:
	default:
		w.logger.Printf("sheets_worker: in-memory queue full, task %d dropped to polling", syncTask.ID)
	}

	return nil
}

// Start launches main loop; stops when ctx is done.
func (w *SheetsWorker) Start(ctx context.Context) {
	w.logger.Printf("sheets_worker: started")
	defer w.logger.Printf("sheets_worker: stopped")

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if t, ok := w.tryLocalQueue(); ok {
			w.processTask(ctx, &t)
			continue
		}

		if t, ok := w.tryRedis(ctx); ok {
			w.processTask(ctx, &t)
			continue
		}

		tasks, err := w.db.GetPendingSyncTasks(ctx, w.batchSize)
		if err != nil {
			w.logger.Printf("sheets_worker: fetch pending: %v", err)
			time.Sleep(w.pollInterval)
			continue
		}
		if len(tasks) == 0 {
			time.Sleep(w.pollInterval)
			continue
		}

		for i := range tasks {
			w.processTask(ctx, &tasks[i])
		}
	}
}

func (w *SheetsWorker) tryLocalQueue() (models.SyncTask, bool) {
	select {
	case t := <-w.queue:
		return t, true
	default:
		return models.SyncTask{}, false
	}
}

func (w *SheetsWorker) tryRedis(ctx context.Context) (models.SyncTask, bool) {
	if w.redis == nil {
		return models.SyncTask{}, false
	}
	res, err := w.redis.BRPop(ctx, time.Second, w.redisQueueKey).Result()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, redis.Nil) {
			return models.SyncTask{}, false
		}
		w.logger.Printf("sheets_worker: redis BRPOP error: %v", err)
		return models.SyncTask{}, false
	}
	if len(res) != 2 {
		return models.SyncTask{}, false
	}
	var task models.SyncTask
	if err := json.Unmarshal([]byte(res[1]), &task); err != nil {
		w.logger.Printf("sheets_worker: decode redis task: %v", err)
		return models.SyncTask{}, false
	}
	return task, true
}

func (w *SheetsWorker) processTask(ctx context.Context, task *models.SyncTask) {
	payload, err := w.decodePayload(task.Payload)
	if err != nil {
		w.failTask(ctx, task, fmt.Errorf("decode payload: %w", err))
		return
	}

	if err := w.handleSheetTask(ctx, task.TaskType, payload); err != nil {
		w.retryOrFail(ctx, task, err)
		return
	}

	if err := w.db.UpdateSyncTaskStatus(ctx, task.ID, "completed", "", nil); err != nil {
		w.logger.Printf("sheets_worker: mark completed %d: %v", task.ID, err)
	}
}

func (w *SheetsWorker) handleSheetTask(ctx context.Context, taskType string, payload sheetTaskPayload) error {
	switch taskType {
	case TaskUpsert:
		if payload.Booking == nil {
			return errors.New("booking payload missing")
		}
		return w.sheets.UpsertBooking(payload.Booking)
	case TaskDelete:
		if payload.BookingID == 0 {
			return errors.New("booking id missing")
		}
		return w.sheets.DeleteBookingRow(payload.BookingID)
	case TaskUpdateStatus:
		if payload.BookingID == 0 || payload.Status == "" {
			return errors.New("booking id or status missing")
		}
		return w.sheets.UpdateBookingStatus(payload.BookingID, payload.Status)
	default:
		return fmt.Errorf("unknown task type: %s", taskType)
	}
}

func (w *SheetsWorker) retryOrFail(ctx context.Context, task *models.SyncTask, cause error) {
	attempt := task.RetryCount + 1
	if attempt >= w.retryPolicy.MaxRetries {
		if err := w.db.UpdateSyncTaskStatus(ctx, task.ID, "failed", cause.Error(), nil); err != nil {
			w.logger.Printf("sheets_worker: mark failed %d: %v", task.ID, err)
		}
		w.pushDeadLetter(ctx, task)
		return
	}

	nextDelay := w.retryPolicy.NextDelay(attempt)
	nextTime := time.Now().Add(nextDelay)
	if err := w.db.UpdateSyncTaskStatus(ctx, task.ID, "retry", cause.Error(), &nextTime); err != nil {
		w.logger.Printf("sheets_worker: mark retry %d: %v", task.ID, err)
	}
}

func (w *SheetsWorker) failTask(ctx context.Context, task *models.SyncTask, err error) {
	if err := w.db.UpdateSyncTaskStatus(ctx, task.ID, "failed", err.Error(), nil); err != nil {
		w.logger.Printf("sheets_worker: mark failed %d: %v", task.ID, err)
	}
	w.pushDeadLetter(ctx, task)
}

func (w *SheetsWorker) decodePayload(raw string) (sheetTaskPayload, error) {
	var payload sheetTaskPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return payload, err
	}
	return payload, nil
}

func (w *SheetsWorker) pushRedis(ctx context.Context, task models.SyncTask) error {
	if w.redis == nil {
		return errors.New("redis client is nil")
	}
	data, err := json.Marshal(task)
	if err != nil {
		return err
	}
	return w.redis.LPush(ctx, w.redisQueueKey, data).Err()
}

func (w *SheetsWorker) pushDeadLetter(ctx context.Context, task *models.SyncTask) {
	if w.redis == nil {
		return
	}
	data, err := json.Marshal(task)
	if err != nil {
		w.logger.Printf("sheets_worker: encode deadletter %d: %v", task.ID, err)
		return
	}
	if err := w.redis.LPush(ctx, w.deadLetterKey, data).Err(); err != nil {
		w.logger.Printf("sheets_worker: deadletter push %d: %v", task.ID, err)
	}
}
