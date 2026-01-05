package service

import (
	"context"
	"errors"
	"time"

	"bronivik/internal/database"
	"bronivik/internal/domain"
	"bronivik/internal/events"
	"bronivik/internal/models"

	"github.com/rs/zerolog"
)

type BookingService struct {
	repo         domain.Repository
	eventBus     domain.EventPublisher
	sheetsWorker domain.SyncWorker
	logger       *zerolog.Logger
}

func NewBookingService(repo domain.Repository, eventBus domain.EventPublisher, sheetsWorker domain.SyncWorker, logger *zerolog.Logger) *BookingService {
	return &BookingService{
		repo:         repo,
		eventBus:     eventBus,
		sheetsWorker: sheetsWorker,
		logger:       logger,
	}
}

func (s *BookingService) CreateBooking(ctx context.Context, booking *models.Booking) error {
	// Проверяем доступность
	available, err := s.repo.CheckAvailability(ctx, booking.ItemID, booking.Date)
	if err != nil {
		return err
	}
	if !available {
		return database.ErrNotAvailable
	}

	// Создаем бронирование с блокировкой
	err = s.repo.CreateBookingWithLock(ctx, booking)
	if err != nil {
		return err
	}

	// Публикуем событие
	s.publishEvent(ctx, events.EventBookingCreated, *booking, "system", 0)

	// Ставим задачу на синхронизацию
	s.enqueueSync(ctx, *booking, "upsert")
	s.sheetsWorker.EnqueueSyncSchedule(ctx, time.Time{}, time.Time{})

	return nil
}

func (s *BookingService) ConfirmBooking(ctx context.Context, bookingID int64, version int64, managerID int64) error {
	err := s.repo.UpdateBookingStatusWithVersion(ctx, bookingID, version, models.StatusConfirmed)
	if err != nil {
		return err
	}

	booking, err := s.repo.GetBooking(ctx, bookingID)
	if err == nil {
		s.publishEvent(ctx, events.EventBookingConfirmed, *booking, "manager", managerID)
		s.enqueueSync(ctx, *booking, "update_status")
		s.sheetsWorker.EnqueueSyncSchedule(ctx, time.Time{}, time.Time{})
	}

	return nil
}

func (s *BookingService) RejectBooking(ctx context.Context, bookingID int64, version int64, managerID int64) error {
	err := s.repo.UpdateBookingStatusWithVersion(ctx, bookingID, version, models.StatusCancelled)
	if err != nil {
		return err
	}

	booking, err := s.repo.GetBooking(ctx, bookingID)
	if err == nil {
		s.publishEvent(ctx, events.EventBookingCancelled, *booking, "manager", managerID)
		s.enqueueSync(ctx, *booking, "update_status")
		s.sheetsWorker.EnqueueSyncSchedule(ctx, time.Time{}, time.Time{})
	}

	return nil
}

func (s *BookingService) CompleteBooking(ctx context.Context, bookingID int64, version int64, managerID int64) error {
	err := s.repo.UpdateBookingStatusWithVersion(ctx, bookingID, version, models.StatusCompleted)
	if err != nil {
		return err
	}

	booking, err := s.repo.GetBooking(ctx, bookingID)
	if err == nil {
		s.publishEvent(ctx, events.EventBookingCompleted, *booking, "manager", managerID)
		s.enqueueSync(ctx, *booking, "update_status")
		s.sheetsWorker.EnqueueSyncSchedule(ctx, time.Time{}, time.Time{})
	}

	return nil
}

func (s *BookingService) ReopenBooking(ctx context.Context, bookingID int64, version int64, managerID int64) error {
	err := s.repo.UpdateBookingStatusWithVersion(ctx, bookingID, version, models.StatusPending)
	if err != nil {
		return err
	}

	booking, err := s.repo.GetBooking(ctx, bookingID)
	if err == nil {
		s.enqueueSync(ctx, *booking, "update_status")
		s.sheetsWorker.EnqueueSyncSchedule(ctx, time.Time{}, time.Time{})
	}

	return nil
}

func (s *BookingService) ChangeBookingItem(ctx context.Context, bookingID int64, version int64, newItemID int64, managerID int64) error {
	// Получаем текущую заявку и проверяем доступность нового аппарата
	_, available, err := s.repo.GetBookingWithAvailability(ctx, bookingID, newItemID)
	if err != nil {
		return err
	}
	if !available {
		return database.ErrNotAvailable
	}

	// Находим имя нового аппарата
	items, err := s.repo.GetActiveItems(ctx)
	if err != nil {
		return err
	}
	var newItemName string
	for _, item := range items {
		if item.ID == newItemID {
			newItemName = item.Name
			break
		}
	}
	if newItemName == "" {
		return errors.New("new item not found")
	}

	err = s.repo.UpdateBookingItemAndStatusWithVersion(ctx, bookingID, version, newItemID, newItemName, models.StatusChanged)
	if err != nil {
		return err
	}

	updatedBooking, err := s.repo.GetBooking(ctx, bookingID)
	if err == nil {
		s.publishEvent(ctx, events.EventBookingItemChange, *updatedBooking, "manager", managerID)
		s.enqueueSync(ctx, *updatedBooking, "upsert")
		s.sheetsWorker.EnqueueSyncSchedule(ctx, time.Time{}, time.Time{})
	}

	return nil
}

func (s *BookingService) RescheduleBooking(ctx context.Context, bookingID int64, managerID int64) error {
	err := s.repo.UpdateBookingStatus(ctx, bookingID, "rescheduled")
	if err != nil {
		return err
	}

	booking, err := s.repo.GetBooking(ctx, bookingID)
	if err == nil {
		s.enqueueSync(ctx, *booking, "update_status")
		s.sheetsWorker.EnqueueSyncSchedule(ctx, time.Time{}, time.Time{})
	}

	return nil
}

func (s *BookingService) GetAvailability(ctx context.Context, itemID int64, startDate time.Time, days int) ([]models.Availability, error) {
	return s.repo.GetAvailabilityForPeriod(ctx, itemID, startDate, days)
}

func (s *BookingService) CheckAvailability(ctx context.Context, itemID int64, date time.Time) (bool, error) {
	return s.repo.CheckAvailability(ctx, itemID, date)
}

func (s *BookingService) publishEvent(ctx context.Context, eventType string, booking models.Booking, changedBy string, changedByID int64) {
	if s.eventBus == nil {
		return
	}

	payload := events.BookingEventPayload{
		BookingID:   booking.ID,
		UserID:      booking.UserID,
		UserName:    booking.UserName,
		ItemID:      booking.ItemID,
		ItemName:    booking.ItemName,
		Status:      booking.Status,
		Date:        booking.Date,
		Comment:     booking.Comment,
		ChangedBy:   changedBy,
		ChangedByID: changedByID,
	}

	if err := s.eventBus.PublishJSON(eventType, payload); err != nil {
		s.logger.Error().Err(err).Str("event_type", eventType).Int64("booking_id", booking.ID).Msg("publish event error")
	}
}

func (s *BookingService) enqueueSync(ctx context.Context, booking models.Booking, taskType string) {
	if s.sheetsWorker == nil {
		return
	}
	
	var status string
	if taskType == "update_status" {
		status = booking.Status
	}

	if err := s.sheetsWorker.EnqueueTask(ctx, taskType, booking.ID, &booking, status); err != nil {
		s.logger.Error().Err(err).Int64("booking_id", booking.ID).Str("task", taskType).Msg("sheets enqueue error")
	}
}
