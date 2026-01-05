package bot

import (
	"context"
	"time"

	"bronivik/internal/models"
)

// SyncUsersToSheets синхронизирует пользователей с Google Sheets
func (b *Bot) SyncUsersToSheets(ctx context.Context) {
	if b.sheetsService == nil {
		return
	}

	users, err := b.userService.GetAllUsers(ctx)
	if err != nil {
		b.logger.Error().Err(err).Msg("Failed to get users for Google Sheets sync")
		return
	}

	var googleUsers []*models.User
	for i := range users {
		user := &users[i]
		googleUsers = append(googleUsers, &models.User{
			ID:            user.ID,
			TelegramID:    user.TelegramID,
			Username:      user.Username,
			FirstName:     user.FirstName,
			LastName:      user.LastName,
			Phone:         user.Phone,
			IsManager:     user.IsManager,
			IsBlacklisted: user.IsBlacklisted,
			LanguageCode:  user.LanguageCode,
			LastActivity:  user.LastActivity,
			CreatedAt:     user.CreatedAt,
			UpdatedAt:     user.UpdatedAt,
		})
	}

	err = b.sheetsService.UpdateUsersSheet(ctx, googleUsers)
	if err != nil {
		b.logger.Error().Err(err).Msg("Failed to sync users to Google Sheets")
	} else {
		b.logger.Info().Msg("Users successfully synced to Google Sheets")
	}
}

// SyncBookingsToSheets синхронизирует бронирования с Google Sheets
func (b *Bot) SyncBookingsToSheets(ctx context.Context) {
	if b.sheetsService == nil {
		b.logger.Warn().Msg("Google Sheets service not initialized")
		return
	}

	// Получаем бронирования за период
	startDate := time.Now().AddDate(0, -models.DefaultExportRangeMonthsBefore, 0)
	endDate := time.Now().AddDate(0, models.DefaultExportRangeMonthsAfter, 0)

	bookings, err := b.bookingService.GetBookingsByDateRange(ctx, startDate, endDate)
	if err != nil {
		b.logger.Error().Err(err).Msg("Failed to get bookings for Google Sheets sync")
		return
	}

	b.logger.Info().Int("count", len(bookings)).Msg("Syncing bookings to Google Sheets")

	// Конвертируем в модели для Google Sheets
	var googleBookings []*models.Booking
	for i := range bookings {
		booking := &bookings[i]
		googleBookings = append(googleBookings, &models.Booking{
			ID:           booking.ID,
			UserID:       booking.UserID,
			ItemID:       booking.ItemID,
			Date:         booking.Date,
			Status:       booking.Status,
			UserName:     booking.UserName,
			Phone:        booking.Phone,
			ItemName:     booking.ItemName,
			Comment:      booking.Comment,
			UserNickname: booking.UserNickname,
			CreatedAt:    booking.CreatedAt,
			UpdatedAt:    booking.UpdatedAt,
		})
	}

	// Полностью перезаписываем лист с заявками
	err = b.sheetsService.ReplaceBookingsSheet(ctx, googleBookings)
	if err != nil {
		b.logger.Error().Err(err).Msg("Failed to sync bookings to Google Sheets")
	} else {
		b.logger.Info().Int("count", len(googleBookings)).Msg("Bookings successfully synced to Google Sheets")
	}

	// Также синхронизируем расписание
	b.SyncScheduleToSheets(ctx)
}

// AppendBookingToSheets добавляет одно бронирование в Google Sheets
func (b *Bot) AppendBookingToSheets(ctx context.Context, booking *models.Booking) {
	if b.sheetsService == nil {
		return
	}

	googleBooking := &models.Booking{
		ID:        booking.ID,
		UserID:    booking.UserID,
		ItemID:    booking.ItemID,
		Date:      booking.Date,
		Status:    booking.Status,
		UserName:  booking.UserName,
		Phone:     booking.Phone,
		ItemName:  booking.ItemName,
		CreatedAt: booking.CreatedAt,
		UpdatedAt: booking.UpdatedAt,
	}

	err := b.sheetsService.AppendBooking(ctx, googleBooking)
	if err != nil {
		b.logger.Error().Err(err).Int64("booking_id", booking.ID).Msg("Failed to append booking to Google Sheets")
	} else {
		b.logger.Info().Int64("booking_id", booking.ID).Msg("Booking appended to Google Sheets")
	}
}

// appendBookingToSheetsAsync отправляет бронирование в Google Sheets с ретраями, не блокируя основной поток.
func (b *Bot) appendBookingToSheetsAsync(ctx context.Context, booking models.Booking) {
	if b.sheetsService == nil {
		return
	}

	go b.retryWithBackoff(ctx, "append booking to sheets", 3, 2*time.Second, func(c context.Context) error {
		return b.sheetsService.AppendBooking(c, &booking)
	})
}

// syncBookingsToSheetsAsync запускает полную синхронизацию с ретраями в фоне.
func (b *Bot) syncBookingsToSheetsAsync(ctx context.Context) {
	if b.sheetsService == nil {
		return
	}

	go b.retryWithBackoff(ctx, "sync bookings to sheets", 2, 5*time.Second, func(c context.Context) error {
		b.SyncBookingsToSheets(c)
		return nil
	})
}

// retryWithBackoff выполняет fn с экспоненциальной задержкой.
func (b *Bot) retryWithBackoff(ctx context.Context, op string, attempts int, baseDelay time.Duration, fn func(context.Context) error) {
	for i := 0; i < attempts; i++ {
		if err := fn(ctx); err != nil {
			b.logger.Warn().
				Err(err).
				Str("operation", op).
				Int("attempt", i+1).
				Int("max_attempts", attempts).
				Msg("Operation attempt failed")

			select {
			case <-ctx.Done():
				return
			case <-time.After(baseDelay * time.Duration(1<<i)):
				continue
			}
		}
		return
	}
	b.logger.Error().Str("operation", op).Int("attempts", attempts).Msg("Operation failed after all attempts")
}

// enqueueBookingUpsert sends an upsert task to the sheets worker if available.
func (b *Bot) enqueueBookingUpsert(ctx context.Context, booking models.Booking) {
	if b.sheetsWorker == nil {
		return
	}
	if err := b.sheetsWorker.EnqueueTask(ctx, "upsert", booking.ID, &booking, ""); err != nil {
		b.logger.Error().Err(err).Int64("booking_id", booking.ID).Msg("sheets enqueue upsert booking error")
	}
}

// enqueueBookingStatus sends a status-only update task to the sheets worker if available.
func (b *Bot) enqueueBookingStatus(ctx context.Context, bookingID int64, status string) {
	if b.sheetsWorker == nil {
		return
	}
	if err := b.sheetsWorker.EnqueueTask(ctx, "update_status", bookingID, nil, status); err != nil {
		b.logger.Error().Err(err).Int64("booking_id", bookingID).Msg("sheets enqueue status booking error")
	}
}
