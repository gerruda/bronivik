package bot

import (
	"context"
	"fmt"
	"time"

	"bronivik/internal/models"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// StartReminders schedules daily reminders for next-day bookings.
func (b *Bot) StartReminders(ctx context.Context) {
	if b == nil || b.tgService == nil {
		return
	}

	go func() {
		// Parse reminder hour from config (default to 9 if invalid)
		hour := 9
		if b.config.Bot.ReminderTime != "" {
			var m int
			_, err := fmt.Sscanf(b.config.Bot.ReminderTime, "%d:%d", &hour, &m)
			if err != nil {
				b.logger.Error().Err(err).Str("reminder_time", b.config.Bot.ReminderTime).Msg("Invalid reminder time format")
				return
			}
		}

		// First wait until next reminder time local time, then tick every 24h.
		wait := timeUntilNextHour(hour)
		timer := time.NewTimer(wait)
		defer timer.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				b.sendTomorrowReminders(ctx)
				timer.Reset(24 * time.Hour)
			}
		}
	}()
}

func (b *Bot) sendTomorrowReminders(ctx context.Context) {
	start := time.Now().Add(24 * time.Hour).Truncate(24 * time.Hour)
	end := start

	bookings, err := b.bookingService.GetBookingsByDateRange(ctx, start, end)
	if err != nil {
		b.logger.Error().Err(err).Time("start", start).Time("end", end).Msg("reminder: get bookings error")
		return
	}

	for _, booking := range bookings {
		if !shouldRemindStatus(booking.Status) {
			continue
		}

		user, err := b.userService.GetUserByID(ctx, booking.UserID)
		if err != nil {
			b.logger.Error().Err(err).Int64("user_id", booking.UserID).Msg("reminder: load user error")
			continue
		}
		if user.TelegramID == 0 {
			continue
		}

		msgText := formatReminderMessage(booking)
		msg := tgbotapi.NewMessage(user.TelegramID, msgText)
		if _, err := b.tgService.Send(msg); err != nil {
			b.logger.Error().Err(err).Int64("telegram_id", user.TelegramID).Msg("reminder: send error")
		}
	}
}

func shouldRemindStatus(status string) bool {
	switch status {
	case models.StatusConfirmed, models.StatusChanged:
		return true
	default:
		return false
	}
}

func formatReminderMessage(b *models.Booking) string {
	date := b.Date.Format("02.01.2006")
	return "Напоминание: завтра у вас бронь " + b.ItemName + " на " + date + ". Статус: " + b.Status
}

func timeUntilNextHour(hour int) time.Duration {
	now := time.Now()
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, now.Location())
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next.Sub(now)
}
