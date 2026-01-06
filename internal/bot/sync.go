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

	// Полностью перезаписываем лист с заявками
	err = b.sheetsService.ReplaceBookingsSheet(ctx, bookings)
	if err != nil {
		b.logger.Error().Err(err).Msg("Failed to sync bookings to Google Sheets")
	} else {
		b.logger.Info().Int("count", len(bookings)).Msg("Bookings successfully synced to Google Sheets")
	}

	// Также синхронизируем расписание
	b.SyncScheduleToSheets(ctx)
}
