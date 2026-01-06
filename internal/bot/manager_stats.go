package bot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bronivik/internal/models"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// getUserStats показывает статистику менеджеру
func (b *Bot) getUserStats(ctx context.Context, update *tgbotapi.Update) {
	if !b.isManager(update.Message.From.ID) {
		return
	}

	allUsers, err := b.userService.GetAllUsers(ctx)
	if err != nil {
		b.logger.Error().Err(err).Msg("Error getting all users")
		b.sendMessage(update.Message.Chat.ID, "Ошибка при получении данных")
		return
	}

	activeUsers, _ := b.userService.GetActiveUsers(ctx, 30)
	managers, _ := b.userService.GetManagers(ctx)

	blacklistedCount := 0
	for _, user := range allUsers {
		if user.IsBlacklisted {
			blacklistedCount++
		}
	}

	// Формируем сообщение со статистикой
	var message strings.Builder
	message.WriteString("📊 *Статистика*\n\n")

	// Пользователи
	message.WriteString("👥 *Пользователи*\n")
	message.WriteString(fmt.Sprintf("Всего: *%d*\n", len(allUsers)))
	message.WriteString(fmt.Sprintf("Активных (30д): *%d*\n", len(activeUsers)))
	message.WriteString(fmt.Sprintf("Менеджеров: *%d*\n", len(managers)))
	message.WriteString(fmt.Sprintf("В черном списке: *%d*\n\n", blacklistedCount))

	message.WriteString("Последние пользователи:\n")
	count := 5
	if len(allUsers) < count {
		count = len(allUsers)
	}
	for i := 0; i < count; i++ {
		user := allUsers[i]
		emoji := "👤"
		if user.IsManager {
			emoji = "👨‍💼"
		} else if user.IsBlacklisted {
			emoji = "🚫"
		}

		message.WriteString(fmt.Sprintf("%s %s %s - %s\n",
			emoji,
			user.FirstName,
			user.LastName,
			user.LastActivity.Format("02.01.2006")))
	}
	message.WriteString("\n")

	// Бронирования
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	periods := []struct {
		label string
		start time.Time
		end   time.Time
	}{
		{"Сегодня", today, today},
		{"7 дней", today.AddDate(0, 0, -6), today},
		{"30 дней", today.AddDate(0, 0, -29), today},
	}

	message.WriteString("📅 *Бронирования*\n")
	for _, p := range periods {
		summary := b.bookingSummary(ctx, p.start, p.end)
		message.WriteString(fmt.Sprintf("%s: %s\n", p.label, summary))
	}

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, message.String())
	msg.ParseMode = models.ParseModeMarkdown

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📤 Экспорт пользователей", "export_users"),
		),
	)
	msg.ReplyMarkup = &keyboard

	if _, err := b.tgService.Send(msg); err != nil {
		b.logger.Error().Err(err).Msg("Failed to send message in getUserStats")
	}
}

// bookingSummary агрегирует заявки за период в компактный блок: всего, статусы, топ-товары.
func (b *Bot) bookingSummary(ctx context.Context, startDate, endDate time.Time) string {
	bookings, err := b.bookingService.GetBookingsByDateRange(ctx, startDate, endDate)
	if err != nil {
		b.logger.Error().Err(err).Msg("bookingSummary error")
		return "ошибка"
	}

	if len(bookings) == 0 {
		return "нет данных"
	}

	statusCount := map[string]int{}
	itemCount := map[string]int{}

	for _, bk := range bookings {
		statusCount[bk.Status]++
		itemCount[bk.ItemName]++
	}

	statusOrder := []string{models.StatusPending, models.StatusConfirmed, models.StatusChanged, models.StatusCompleted, models.StatusCanceled}
	statusParts := make([]string, 0, len(statusOrder))
	for _, st := range statusOrder {
		if c := statusCount[st]; c > 0 {
			statusParts = append(statusParts, fmt.Sprintf("%s:%d", st, c))
		}
	}

	type kv struct {
		name  string
		count int
	}
	items := make([]kv, 0, len(itemCount))
	for name, c := range itemCount {
		items = append(items, kv{name: name, count: c})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count == items[j].count {
			return items[i].name < items[j].name
		}
		return items[i].count > items[j].count
	})
	if len(items) > 3 {
		items = items[:3]
	}
	itemParts := make([]string, 0, 3)
	for _, it := range items {
		itemParts = append(itemParts, fmt.Sprintf("%s:%d", it.name, it.count))
	}

	return fmt.Sprintf("всего %d | статусы [%s] | топ [%s]",
		len(bookings),
		strings.Join(statusParts, ", "),
		strings.Join(itemParts, ", "),
	)
}

// handleExportUsers обработка экспорта пользователей
func (b *Bot) handleExportUsers(ctx context.Context, update *tgbotapi.Update) {
	callback := update.CallbackQuery
	if callback == nil || !b.isManager(callback.From.ID) {
		return
	}

	users, err := b.userService.GetAllUsers(ctx)
	if err != nil {
		b.logger.Error().Err(err).Msg("Error getting users for export")
		b.sendMessage(callback.Message.Chat.ID, "Ошибка при получении данных пользователей")
		return
	}

	filePath, err := b.exportUsersToExcel(ctx, users)
	if err != nil {
		b.logger.Error().Err(err).Msg("Error exporting users to Excel")
		b.sendMessage(callback.Message.Chat.ID, "Ошибка при создании файла экспорта")
		return
	}

	// Отправляем файл
	file, err := os.Open(filePath)
	if err != nil {
		b.logger.Error().Err(err).Str("file_path", filePath).Msg("Error opening file")
		b.sendMessage(callback.Message.Chat.ID, "Ошибка при открытии файла")
		return
	}
	defer file.Close()

	fileReader := tgbotapi.FileReader{
		Name:   filepath.Base(filePath),
		Reader: file,
	}

	doc := tgbotapi.NewDocument(callback.Message.Chat.ID, fileReader)
	doc.Caption = "📊 Экспорт данных пользователей"

	_, err = b.tgService.Send(doc)
	if err != nil {
		b.logger.Error().Err(err).Msg("Error sending document")
		b.sendMessage(callback.Message.Chat.ID, "Ошибка при отправке файла")
		return
	}

	b.sendMessage(callback.Message.Chat.ID, "✅ Файл с пользователями успешно отправлен")
}
