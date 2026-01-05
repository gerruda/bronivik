package bot

import (
	"context"
	"time"

	"bronivik/internal/models"
)

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
