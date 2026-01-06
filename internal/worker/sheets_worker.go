package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"bronivik/internal/database"
	"bronivik/internal/models"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

const (
	TaskUpsert       = "upsert"
	TaskDelete       = "delete"
	TaskUpdateStatus = "update_status"
	TaskSyncSchedule = "sync_schedule"
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
	StartDate time.Time       `json:"start_date,omitempty"`
	EndDate   time.Time       `json:"end_date,omitempty"`
}

// SheetsWorker consumes sync_queue tasks and applies them to Google Sheets.
type SheetsClient interface {
	UpsertBooking(context.Context, *models.Booking) error
	DeleteBookingRow(context.Context, int64) error
	UpdateBookingStatus(context.Context, int64, string) error
	UpdateScheduleSheet(
		ctx context.Context,
		startDate, endDate time.Time,
		dailyBookings map[string][]*models.Booking,
		items []*models.Item,
	) error
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
	logger        *zerolog.Logger
}

// NewSheetsWorker builds a worker with sane defaults.
func NewSheetsWorker(
	db *database.DB,
	sheets SheetsClient,
	redisClient *redis.Client,
	retry RetryPolicy,
	logger *zerolog.Logger,
) *SheetsWorker {
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
		l := zerolog.New(os.Stdout).With().Timestamp().Logger()
		logger = &l
	}

	return &SheetsWorker{
		db:            db,
		sheets:        sheets,
		redis:         redisClient,
		retryPolicy:   retry,
		queue:         make(chan models.SyncTask, models.WorkerQueueSize),
		redisQueueKey: "sheets:queue",
		deadLetterKey: "sheets:deadletter",
		pollInterval:  2 * time.Second,
		batchSize:     20,
		logger:        logger,
	}
}

// EnqueueTask persists task to DB and schedules it via redis or in-memory queue.
func (w *SheetsWorker) EnqueueTask(ctx context.Context, taskType string, bookingID int64, booking *models.Booking, status string) error {
	if taskType == "" {
		return errors.New("task type is required")
	}
	if bookingID == 0 && (booking == nil || booking.ID == 0) {
		return errors.New("booking id is required")
	}

	payload := sheetTaskPayload{
		BookingID: bookingID,
		Booking:   booking,
		Status:    status,
	}
	if payload.BookingID == 0 && booking != nil {
		payload.BookingID = booking.ID
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode payload: %w", err)
	}

	syncTask := models.SyncTask{
		TaskType:  taskType,
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
		if err := w.pushRedis(ctx, &syncTask); err != nil {
			w.logger.Warn().Err(err).Msg("sheets_worker: redis push failed, fallback to memory queue")
		} else {
			return nil
		}
	}

	// Fallback to in-memory queue if redis missing or failed.
	select {
	case w.queue <- syncTask:
	default:
		w.logger.Warn().Int64("task_id", syncTask.ID).Msg("sheets_worker: in-memory queue full, task dropped to polling")
	}

	return nil
}

// Start launches main loop; stops when ctx is done.
func (w *SheetsWorker) Start(ctx context.Context) {
	w.logger.Info().Msg("sheets_worker: started")
	defer w.logger.Info().Msg("sheets_worker: stopped")

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
			w.logger.Error().Err(err).Msg("sheets_worker: fetch pending")
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
		w.logger.Error().Err(err).Msg("sheets_worker: redis BRPOP error")
		return models.SyncTask{}, false
	}
	if len(res) != 2 {
		return models.SyncTask{}, false
	}
	var task models.SyncTask
	if err := json.Unmarshal([]byte(res[1]), &task); err != nil {
		w.logger.Error().Err(err).Msg("sheets_worker: decode redis task")
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

	if err := w.handleSheetTask(ctx, task.TaskType, &payload); err != nil {
		w.retryOrFail(ctx, task, err)
		return
	}

	if err := w.db.UpdateSyncTaskStatus(ctx, task.ID, "completed", "", nil); err != nil {
		w.logger.Error().Err(err).Int64("task_id", task.ID).Msg("sheets_worker: mark completed")
	}
}

func (w *SheetsWorker) handleSheetTask(ctx context.Context, taskType string, payload *sheetTaskPayload) error {
	switch taskType {
	case TaskUpsert:
		if payload.Booking == nil {
			return errors.New("booking payload missing")
		}
		return w.sheets.UpsertBooking(ctx, payload.Booking)
	case TaskDelete:
		if payload.BookingID == 0 {
			return errors.New("booking id missing")
		}
		return w.sheets.DeleteBookingRow(ctx, payload.BookingID)
	case TaskUpdateStatus:
		if payload.BookingID == 0 || payload.Status == "" {
			return errors.New("booking id or status missing")
		}
		return w.sheets.UpdateBookingStatus(ctx, payload.BookingID, payload.Status)
	case TaskSyncSchedule:
		startDate := payload.StartDate
		endDate := payload.EndDate
		if startDate.IsZero() {
			startDate = time.Now().AddDate(0, -models.DefaultExportRangeMonthsBefore, 0).Truncate(24 * time.Hour)
		}
		if endDate.IsZero() {
			endDate = time.Now().AddDate(0, models.DefaultExportRangeMonthsAfter, 0).Truncate(24 * time.Hour)
		}

		dailyBookings, err := w.db.GetDailyBookings(ctx, startDate, endDate)
		if err != nil {
			return fmt.Errorf("get daily bookings: %w", err)
		}

		items, err := w.db.GetActiveItems(ctx)
		if err != nil {
			return fmt.Errorf("get active items: %w", err)
		}

		return w.sheets.UpdateScheduleSheet(ctx, startDate, endDate, dailyBookings, items)
	default:
		return fmt.Errorf("unknown task type: %s", taskType)
	}
}

func (w *SheetsWorker) retryOrFail(ctx context.Context, task *models.SyncTask, cause error) {
	attempt := task.RetryCount + 1
	if attempt >= w.retryPolicy.MaxRetries {
		if err := w.db.UpdateSyncTaskStatus(ctx, task.ID, "failed", cause.Error(), nil); err != nil {
			w.logger.Error().Err(err).Int64("task_id", task.ID).Msg("sheets_worker: mark failed")
		}
		w.pushDeadLetter(ctx, task)
		return
	}

	nextDelay := w.retryPolicy.NextDelay(attempt)
	nextTime := time.Now().Add(nextDelay)
	if uerr := w.db.UpdateSyncTaskStatus(ctx, task.ID, "retry", cause.Error(), &nextTime); uerr != nil {
		w.logger.Error().Err(uerr).Int64("task_id", task.ID).Msg("sheets_worker: mark retry")
	}
}

func (w *SheetsWorker) failTask(ctx context.Context, task *models.SyncTask, err error) {
	if uerr := w.db.UpdateSyncTaskStatus(ctx, task.ID, "failed", err.Error(), nil); uerr != nil {
		w.logger.Error().Err(uerr).Int64("task_id", task.ID).Msg("sheets_worker: mark failed")
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

func (w *SheetsWorker) pushRedis(ctx context.Context, task *models.SyncTask) error {
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
		w.logger.Error().Err(err).Int64("task_id", task.ID).Msg("sheets_worker: encode deadletter")
		return
	}
	if err := w.redis.LPush(ctx, w.deadLetterKey, data).Err(); err != nil {
		w.logger.Error().Err(err).Int64("task_id", task.ID).Msg("sheets_worker: deadletter push")
	}
}

func (w *SheetsWorker) EnqueueSyncSchedule(ctx context.Context, startDate, endDate time.Time) error {
	payload := sheetTaskPayload{
		StartDate: startDate,
		EndDate:   endDate,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode payload: %w", err)
	}

	syncTask := models.SyncTask{
		TaskType:  TaskSyncSchedule,
		Payload:   string(payloadBytes),
		Status:    "pending",
		CreatedAt: time.Now(),
	}

	if err := w.db.CreateSyncTask(ctx, &syncTask); err != nil {
		return fmt.Errorf("persist sync task: %w", err)
	}

	if w.redis != nil {
		if err := w.pushRedis(ctx, &syncTask); err != nil {
			w.logger.Warn().Err(err).Msg("sheets_worker: redis push failed, fallback to memory queue")
		} else {
			return nil
		}
	}

	select {
	case w.queue <- syncTask:
	default:
		w.logger.Warn().Int64("task_id", syncTask.ID).Msg("sheets_worker: in-memory queue full, task dropped to polling")
	}

	return nil
}
