package bot

import (
	"context"
	"time"

	"bronivik/internal/models"
)

// SyncScheduleToSheets синхронизирует расписание в формате таблицы с Google Sheets
func (b *Bot) SyncScheduleToSheets(ctx context.Context) {
	if b.sheetsService == nil {
		b.logger.Warn().Msg("Google Sheets service not initialized")
		return
	}

	// Определяем период
	startDate := time.Now().AddDate(0, -models.DefaultExportRangeMonthsBefore, 0).Truncate(24 * time.Hour)
	endDate := time.Now().AddDate(0, models.DefaultExportRangeMonthsAfter, 0).Truncate(24 * time.Hour)

	b.logger.Info().
		Time("start_date", startDate).
		Time("end_date", endDate).
		Msg("Syncing schedule to Google Sheets")

	// Получаем данные о бронированиях
	dailyBookings, err := b.bookingService.GetDailyBookings(ctx, startDate, endDate)
	if err != nil {
		b.logger.Error().Err(err).Msg("Failed to get daily bookings for schedule sync")
		return
	}

	// Логируем количество найденных бронирований
	totalBookings := 0
	for _, bookings := range dailyBookings {
		totalBookings += len(bookings)
	}
	b.logger.Info().
		Int("total_bookings", totalBookings).
		Int("dates_count", len(dailyBookings)).
		Msg("Found bookings for sync")

	// Конвертируем модели
	googleDailyBookings := make(map[string][]models.Booking)
	for date, bookings := range dailyBookings {
		var googleBookings []models.Booking
		for _, booking := range bookings {
			googleBookings = append(googleBookings, models.Booking{
				ID:           booking.ID,
				UserID:       booking.UserID,
				ItemID:       booking.ItemID,
				Date:         booking.Date,
				Status:       booking.Status,
				Comment:      booking.Comment,
				UserName:     booking.UserName,
				UserNickname: booking.UserNickname,
				Phone:        booking.Phone,
				ItemName:     booking.ItemName,
				CreatedAt:    booking.CreatedAt,
				UpdatedAt:    booking.UpdatedAt,
			})
		}
		googleDailyBookings[date] = googleBookings
	}

	// Конвертируем items
	var googleItems []models.Item
	items, err := b.itemService.GetActiveItems(ctx)
	if err != nil {
		b.logger.Error().Err(err).Msg("Failed to get active items for schedule sync")
		return
	}
	for _, item := range items {
		googleItems = append(googleItems, models.Item{
			ID:            item.ID,
			Name:          item.Name,
			TotalQuantity: item.TotalQuantity,
		})
	}

	b.logger.Info().Int("items_count", len(googleItems)).Msg("Updating Google Sheets")

	// Обновляем расписание в Google Sheets
	err = b.sheetsService.UpdateScheduleSheet(ctx, startDate, endDate, googleDailyBookings, googleItems)
	if err != nil {
		b.logger.Error().Err(err).Msg("Failed to sync schedule to Google Sheets")
	} else {
		b.logger.Info().Msg("Schedule successfully synced to Google Sheets")
	}
}
