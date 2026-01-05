package bot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"bronivik/internal/database"
	"bronivik/internal/events"
	"bronivik/internal/models"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// handleManagerCommand обработка команд менеджера
func (b *Bot) handleManagerCommand(ctx context.Context, update tgbotapi.Update) bool {
	if !b.isManager(update.Message.From.ID) {
		return false
	}

	userID := update.Message.From.ID
	text := update.Message.Text
	state := b.getUserState(ctx, userID)

	switch {
	case text == "👨‍💼 Все заявки":
		b.showManagerBookings(ctx, update)

	case text == "/get_all":
		b.showManagerBookings(ctx, update)

	case text == "➕ Создать заявку (Менеджер)":
		b.startManagerBooking(ctx, update)

	// секретная команда, доступная менеджерам, но не отображаемся у них в меню
	case text == "/stats" && b.isManager(userID):
		b.getUserStats(ctx, update)

	case strings.HasPrefix(text, "/manager_booking_"):
		// Просмотр конкретной заявки
		parts := strings.Split(text, "_")
		if len(parts) >= 3 {
			bookingID, err := strconv.ParseInt(parts[2], 10, 64)
			if err == nil {
				b.showManagerBookingDetail(ctx, update, bookingID)
			}
		}

	case state != nil && state.CurrentStep == "manager_waiting_client_name":
		b.handleManagerClientName(ctx, update, text, state)

	case state != nil && state.CurrentStep == "manager_waiting_client_phone":
		b.handleManagerClientPhone(ctx, update, text, state)

	case state != nil && state.CurrentStep == "manager_waiting_single_date":
		b.handleManagerSingleDate(ctx, update, text, state)

	case state != nil && state.CurrentStep == "manager_waiting_start_date":
		b.handleManagerStartDate(ctx, update, text, state)

	case state != nil && state.CurrentStep == "manager_waiting_end_date":
		b.handleManagerEndDate(ctx, update, text, state)

	case state != nil && state.CurrentStep == "manager_waiting_comment":
		b.handleManagerComment(ctx, update, text, state)

	case state != nil && state.CurrentStep == "manager_confirm_booking" && text == "✅ Подтвердить создание":
		b.createManagerBookings(ctx, update, state)

	case state != nil && state.CurrentStep == "manager_confirm_booking" && text == "❌ Отмена":
		b.clearUserState(ctx, update.Message.From.ID)
		b.sendMessage(update.Message.Chat.ID, "❌ Создание заявки отменено")
		b.handleMainMenu(ctx, update)

	case text == "🔄 Синхронизировать бронирования (Google Sheets)":
		b.sendMessage(update.Message.Chat.ID, "⏳ Запускаю фоновую синхронизацию бронирований...")
		go b.SyncBookingsToSheets(ctx)

	case text == "📅 Синхронизировать расписание (Google Sheets)":
		b.sendMessage(update.Message.Chat.ID, "⏳ Запускаю фоновую синхронизацию расписания...")
		go b.SyncScheduleToSheets(ctx)

	case strings.HasPrefix(text, "/add_item"):
		b.handleAddItemCommand(ctx, update)
		return true

	case strings.HasPrefix(text, "/edit_item"):
		b.handleEditItemCommand(ctx, update)
		return true

	case strings.HasPrefix(text, "/list_items"):
		b.handleListItemsCommand(ctx, update)
		return true

	case strings.HasPrefix(text, "/disable_item"):
		b.handleDisableItemCommand(ctx, update)
		return true

	case strings.HasPrefix(text, "/set_item_order"):
		b.handleSetItemOrderCommand(ctx, update)
		return true

	case strings.HasPrefix(text, "/move_item_up"):
		b.handleMoveItemCommand(ctx, update, -1)
		return true

	case strings.HasPrefix(text, "/move_item_down"):
		b.handleMoveItemCommand(ctx, update, 1)
		return true
	}

	return false
}

// handleManagerCallback обработка действий менеджера с заявками
func (b *Bot) handleManagerCallback(ctx context.Context, update tgbotapi.Update) bool {
	callback := update.CallbackQuery
	if callback == nil {
		return false
	}

	data := callback.Data
	var bookingID int64
	var action string

	// Обрабатываем все возможные действия
	actions := []string{"confirm_", "reject_", "reschedule_", "change_item_", "reopen_", "complete_"}
	for _, act := range actions {
		if _, err := fmt.Sscanf(data, act+"%d", &bookingID); err == nil {
			action = act
			break
		}
	}

	if action == "" {
		// Проверяем другие действия менеджера
		if data == "export_users" {
			b.handleExportUsers(ctx, update)
			return true
		}
		return false
	}

	booking, err := b.db.GetBooking(ctx, bookingID)
	if err != nil {
		b.logger.Error().Err(err).Int64("booking_id", bookingID).Msg("Error getting booking")
		return true
	}

	switch action {
	case "confirm_":
		b.confirmBooking(ctx, booking, callback.Message.Chat.ID)
	case "reject_":
		b.rejectBooking(ctx, booking, callback.Message.Chat.ID)
	case "reschedule_":
		b.rescheduleBooking(ctx, booking, callback.Message.Chat.ID)
	case "change_item_":
		b.startChangeItem(ctx, booking, callback.Message.Chat.ID)
	case "reopen_":
		b.reopenBooking(ctx, booking, callback.Message.Chat.ID)
	case "complete_":
		b.completeBooking(ctx, booking, callback.Message.Chat.ID)
	}

	// Обновляем сообщение у менеджера
	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID,
		fmt.Sprintf("✅ Заявка #%d обработана\nДействие: %s", bookingID, action))
	b.bot.Send(editMsg)

	return true
}

// startManagerBooking начало создания заявки менеджером
func (b *Bot) startManagerBooking(ctx context.Context, update tgbotapi.Update) {
	if !b.isManager(update.Message.From.ID) {
		return
	}

	msg := tgbotapi.NewMessage(update.Message.Chat.ID,
		"📋 Создание заявки от имени клиента\n\nВведите Имя клиента:")

	b.setUserState(ctx, update.Message.From.ID, "manager_waiting_client_name", map[string]interface{}{
		"is_manager_booking": true,
	})
	b.bot.Send(msg)
}

// handleManagerClientName обработка ввода имени клиента
func (b *Bot) handleManagerClientName(ctx context.Context, update tgbotapi.Update, text string, state *models.UserState) {
	state.TempData["client_name"] = text
	b.setUserState(ctx, update.Message.From.ID, "manager_waiting_client_phone", state.TempData)

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, "📱 Введите телефон клиента:")
	b.bot.Send(msg)
}

// handleManagerClientPhone обработка ввода телефона клиента
func (b *Bot) handleManagerClientPhone(ctx context.Context, update tgbotapi.Update, text string, state *models.UserState) {
	// Нормализуем телефон
	normalizedPhone := b.normalizePhone(text)
	if normalizedPhone == "" {
		b.sendMessage(update.Message.Chat.ID, "Неверный формат номера телефона. Пожалуйста, введите номер в формате +7XXXXXXXXXX или 8XXXXXXXXXX")
		return
	}

	state.TempData["client_phone"] = normalizedPhone
	b.setUserState(ctx, update.Message.From.ID, "manager_waiting_item_selection", state.TempData)

	// Показываем выбор аппарата с пагинацией
	b.sendManagerItemsPage(ctx, update.Message.Chat.ID, update.Message.From.ID, 0)
}

// sendManagerItemsPage отправляет страницу с аппаратами для менеджера
func (b *Bot) sendManagerItemsPage(ctx context.Context, chatID, userID int64, page int) {
	itemsPerPage := 8
	startIdx := page * itemsPerPage
	endIdx := startIdx + itemsPerPage
	if endIdx > len(b.items) {
		endIdx = len(b.items)
	}

	var message strings.Builder
	message.WriteString("🏢 *Выберите аппарат:*\n\n")
	message.WriteString(fmt.Sprintf("Страница %d из %d\n\n", page+1, (len(b.items)+itemsPerPage-1)/itemsPerPage))

	currentItems := b.items[startIdx:endIdx]
	for i, item := range currentItems {
		message.WriteString(fmt.Sprintf("%d. *%s*\n", startIdx+i+1, item.Name))
		message.WriteString(fmt.Sprintf("   📝 %s\n", item.Description))
		message.WriteString(fmt.Sprintf("   👥 Вместимость: %d чел.\n\n", item.TotalQuantity))
	}

	var keyboard [][]tgbotapi.InlineKeyboardButton

	for i, item := range currentItems {
		btn := tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("%d. %s", startIdx+i+1, item.Name),
			fmt.Sprintf("manager_select_item:%d", item.ID),
		)
		keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{btn})
	}

	var navButtons []tgbotapi.InlineKeyboardButton

	if page > 0 {
		navButtons = append(navButtons, tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", fmt.Sprintf("manager_items_page:%d", page-1)))
	}

	if endIdx < len(b.items) {
		navButtons = append(navButtons, tgbotapi.NewInlineKeyboardButtonData("Вперед ➡️", fmt.Sprintf("manager_items_page:%d", page+1)))
	}

	if len(navButtons) > 0 {
		keyboard = append(keyboard, navButtons)
	}

	markup := tgbotapi.NewInlineKeyboardMarkup(keyboard...)

	msg := tgbotapi.NewMessage(chatID, message.String())
	msg.ReplyMarkup = &markup
	msg.ParseMode = "Markdown"

	b.bot.Send(msg)
}

// handleManagerItemSelection обработка выбора аппарата менеджером
func (b *Bot) handleManagerItemSelection(ctx context.Context, update tgbotapi.Update) {
	callback := update.CallbackQuery
	data := callback.Data

	itemIDStr := strings.TrimPrefix(data, "manager_select_item:")
	itemID, err := strconv.ParseInt(itemIDStr, 10, 64)
	if err != nil {
		b.logger.Error().Err(err).Str("item_id_str", itemIDStr).Msg("Error parsing item ID")
		return
	}

	var selectedItem models.Item
	for _, item := range b.items {
		if item.ID == itemID {
			selectedItem = item
			break
		}
	}

	if selectedItem.ID == 0 {
		b.sendMessage(callback.Message.Chat.ID, "Аппарат не найден")
		return
	}

	state := b.getUserState(ctx, callback.From.ID)
	if state == nil {
		b.sendMessage(callback.Message.Chat.ID, "Сессия устарела. Начните заново.")
		return
	}

	state.TempData["item_id"] = selectedItem.ID
	b.setUserState(ctx, callback.From.ID, "manager_waiting_date_type", state.TempData)

	// Спрашиваем тип даты (одна дата или интервал)
	msg := tgbotapi.NewMessage(callback.Message.Chat.ID, "📅 Выберите тип бронирования:")

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📅 Одна дата", "manager_single_date"),
			tgbotapi.NewInlineKeyboardButtonData("📆 Интервал дат", "manager_date_range"),
		),
	)
	msg.ReplyMarkup = &keyboard

	b.bot.Send(msg)
	b.bot.Send(tgbotapi.NewCallback(callback.ID, ""))
}

// handleManagerDateType обработка выбора типа даты
func (b *Bot) handleManagerDateType(ctx context.Context, update tgbotapi.Update, dateType string) {
	callback := update.CallbackQuery
	state := b.getUserState(ctx, callback.From.ID)
	if state == nil {
		return
	}

	if dateType == "single" {
		state.TempData["date_type"] = "single"
		b.setUserState(ctx, callback.From.ID, "manager_waiting_single_date", state.TempData)

		editMsg := tgbotapi.NewEditMessageText(
			callback.Message.Chat.ID,
			callback.Message.MessageID,
			"📅 Введите дату бронирования в формате ДД.ММ.ГГГГ (например, 25.12.2024):",
		)
		b.bot.Send(editMsg)
	} else {
		state.TempData["date_type"] = "range"
		b.setUserState(ctx, callback.From.ID, "manager_waiting_start_date", state.TempData)

		editMsg := tgbotapi.NewEditMessageText(
			callback.Message.Chat.ID,
			callback.Message.MessageID,
			"📅 Введите начальную дату интервала в формате ДД.ММ.ГГГГ (например, 25.12.2024):",
		)
		b.bot.Send(editMsg)
	}

	b.bot.Send(tgbotapi.NewCallback(callback.ID, ""))
}

// handleManagerSingleDate обработка ввода одной даты
func (b *Bot) handleManagerSingleDate(ctx context.Context, update tgbotapi.Update, dateStr string, state *models.UserState) {
	date, err := time.Parse("02.01.2006", dateStr)
	if err != nil {
		b.sendMessage(update.Message.Chat.ID, "Неверный формат даты. Используйте ДД.ММ.ГГГГ (например, 25.12.2024)")
		return
	}

	// Проверяем, что дата не в прошлом
	if date.Before(time.Now().AddDate(0, 0, -1)) {
		b.sendMessage(update.Message.Chat.ID, "Нельзя бронировать на прошедшие даты. Выберите будущую дату.")
		return
	}

	state.TempData["dates"] = []time.Time{date}
	b.setUserState(ctx, update.Message.From.ID, "manager_waiting_comment", state.TempData)

	b.sendMessage(update.Message.Chat.ID, "💬 Введите комментарий к заявке (например: 'Техническое обслуживание', 'Обучение персонала' или любой другой текст):")
}

// handleManagerStartDate обработка ввода начальной даты интервала
func (b *Bot) handleManagerStartDate(ctx context.Context, update tgbotapi.Update, dateStr string, state *models.UserState) {
	startDate, err := time.Parse("02.01.2006", dateStr)
	if err != nil {
		b.sendMessage(update.Message.Chat.ID, "Неверный формат даты. Используйте ДД.ММ.ГГГГ (например, 25.12.2024)")
		return
	}

	// Проверяем, что дата не в прошлом
	if startDate.Before(time.Now().AddDate(0, 0, -1)) {
		b.sendMessage(update.Message.Chat.ID, "Нельзя бронировать на прошедшие даты. Выберите будущую дату.")
		return
	}

	state.TempData["start_date"] = startDate
	b.setUserState(ctx, update.Message.From.ID, "manager_waiting_end_date", state.TempData)

	b.sendMessage(update.Message.Chat.ID, "📅 Введите конечную дату интервала в формате ДД.ММ.ГГГГ:")
}

// handleManagerEndDate обработка ввода конечной даты интервала
func (b *Bot) handleManagerEndDate(ctx context.Context, update tgbotapi.Update, dateStr string, state *models.UserState) {
	endDate, err := time.Parse("02.01.2006", dateStr)
	if err != nil {
		b.sendMessage(update.Message.Chat.ID, "Неверный формат даты. Используйте ДД.ММ.ГГГГ (например, 25.12.2024)")
		return
	}

	startDate := b.getTimeFromTempData(state.TempData, "start_date")

	// Проверяем, что конечная дата не раньше начальной
	if endDate.Before(startDate) {
		b.sendMessage(update.Message.Chat.ID, "Конечная дата не может быть раньше начальной.")
		return
	}

	// Создаем список всех дат в интервале
	var dates []time.Time
	for d := startDate; !d.After(endDate); d = d.AddDate(0, 0, 1) {
		dates = append(dates, d)
	}

	state.TempData["dates"] = dates
	b.setUserState(ctx, update.Message.From.ID, "manager_waiting_comment", state.TempData)

	b.sendMessage(update.Message.Chat.ID, fmt.Sprintf("💬 Введите комментарий к заявке (будет применен ко всем %d дням):", len(dates)))
}

// handleManagerComment обработка ввода комментария
func (b *Bot) handleManagerComment(ctx context.Context, update tgbotapi.Update, comment string, state *models.UserState) {
	state.TempData["comment"] = comment
	b.setUserState(ctx, update.Message.From.ID, "manager_confirm_booking", state.TempData)

	// Показываем подтверждение
	b.showManagerBookingConfirmation(ctx, update, state)
}

// showManagerBookingConfirmation показывает подтверждение заявки менеджером
func (b *Bot) showManagerBookingConfirmation(ctx context.Context, update tgbotapi.Update, state *models.UserState) {
	clientName := state.TempData["client_name"].(string)
	clientPhone := state.TempData["client_phone"].(string)
	itemID := b.getInt64FromTempData(state.TempData, "item_id")
	var selectedItem models.Item
	for _, item := range b.items {
		if item.ID == itemID {
			selectedItem = item
			break
		}
	}
	dates := b.getDatesFromTempData(state.TempData, "dates")
	comment := state.TempData["comment"].(string)
	dateType := state.TempData["date_type"].(string)

	var message strings.Builder
	message.WriteString("📋 *Подтверждение заявки:*\n\n")
	message.WriteString(fmt.Sprintf("👤 *Клиент:* %s\n", clientName))
	message.WriteString(fmt.Sprintf("📱 *Телефон:* %s\n", clientPhone))
	message.WriteString(fmt.Sprintf("🏢 *Аппарат:* %s\n", selectedItem.Name))

	if dateType == "single" {
		message.WriteString(fmt.Sprintf("📅 *Дата:* %s\n", dates[0].Format("02.01.2006")))
	} else {
		message.WriteString(fmt.Sprintf("📅 *Интервал:* %s - %s (%d дней)\n",
			dates[0].Format("02.01.2006"),
			dates[len(dates)-1].Format("02.01.2006"),
			len(dates)))
	}

	message.WriteString(fmt.Sprintf("💬 *Комментарий:* %s\n\n", comment))

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, message.String())

	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("✅ Подтвердить создание"),
			tgbotapi.NewKeyboardButton("❌ Отмена"),
		),
	)
	msg.ReplyMarkup = keyboard
	msg.ParseMode = "Markdown"

	b.bot.Send(msg)
}

// createManagerBookings создает заявки менеджера
func (b *Bot) createManagerBookings(ctx context.Context, update tgbotapi.Update, state *models.UserState) {
	clientName := state.TempData["client_name"].(string)
	clientPhone := state.TempData["client_phone"].(string)
	itemID := b.getInt64FromTempData(state.TempData, "item_id")
	var selectedItem models.Item
	for _, item := range b.items {
		if item.ID == itemID {
			selectedItem = item
			break
		}
	}
	dates := b.getDatesFromTempData(state.TempData, "dates")
	comment := state.TempData["comment"].(string)

	var createdBookings []*models.Booking
	var failedDates []string

	// Создаем заявки на каждую дату
	for _, date := range dates {
		// Проверяем доступность
		available, err := b.db.CheckAvailability(ctx, selectedItem.ID, date)
		if err != nil {
			b.logger.Error().Err(err).Int64("item_id", selectedItem.ID).Time("date", date).Msg("Error checking availability")
			failedDates = append(failedDates, date.Format("02.01.2006"))
			continue
		}

		if !available {
			failedDates = append(failedDates, date.Format("02.01.2006"))
			continue
		}

		// Создаем бронирование
		booking := &models.Booking{
			UserID:       update.Message.From.ID, // ID менеджера
			UserName:     clientName,
			UserNickname: clientName,
			Phone:        clientPhone,
			ItemID:       selectedItem.ID,
			ItemName:     selectedItem.Name,
			Date:         date,
			Status:       models.StatusConfirmed, // Менеджер создает сразу подтвержденные заявки
			Comment:      comment,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}

		err = b.db.CreateBooking(ctx, booking)
		if err != nil {
			b.logger.Error().Err(err).Interface("booking", booking).Msg("Error creating manager booking")
			failedDates = append(failedDates, date.Format("02.01.2006"))
		} else {
			createdBookings = append(createdBookings, booking)
			b.publishBookingEvent(ctx, events.EventBookingCreated, *booking, "manager", update.Message.From.ID)
			b.publishBookingEvent(ctx, events.EventBookingConfirmed, *booking, "manager", update.Message.From.ID)
		}
	}

	// Формируем отчет
	var message strings.Builder
	message.WriteString("📊 *Результат создания заявок:*\n\n")

	if len(createdBookings) > 0 {
		message.WriteString(fmt.Sprintf("✅ *Успешно создано:* %d заявок\n", len(createdBookings)))
		for _, booking := range createdBookings {
			message.WriteString(fmt.Sprintf("   • %s (№%d)\n", booking.Date.Format("02.01.2006"), booking.ID))
		}
		message.WriteString("\n")
	}

	if len(failedDates) > 0 {
		message.WriteString(fmt.Sprintf("❌ *Не удалось создать:* %d заявок\n", len(failedDates)))
		for _, date := range failedDates {
			message.WriteString(fmt.Sprintf("   • %s (недоступно)\n", date))
		}
	}

	b.sendMessage(update.Message.Chat.ID, message.String())

	// Очищаем состояние
	b.clearUserState(ctx, update.Message.From.ID)

	if len(createdBookings) > 0 {
		// Асинхронно обновляем расписание после пакетного создания
		go b.SyncScheduleToSheets(ctx)
	}

	// Возвращаем в главное меню
	b.handleMainMenu(ctx, update)
}

// showManagerBookings показывает все заявки менеджеру
func (b *Bot) showManagerBookings(ctx context.Context, update tgbotapi.Update) {
	if !b.isManager(update.Message.From.ID) {
		return
	}

	// Получаем все заявки за период: один месяц назад и два месяца вперед
	startDate := time.Now().AddDate(0, 0, -7) // 7 дней месяц назад
	endDate := time.Now().AddDate(0, 2, 0)    // 2 месяца вперед

	bookings, err := b.db.GetBookingsByDateRange(ctx, startDate, endDate)
	if err != nil {
		b.logger.Error().Err(err).Time("start_date", startDate).Time("end_date", endDate).Msg("Error getting bookings")
		b.sendMessage(update.Message.Chat.ID, "Ошибка при получении заявок")
		return
	}

	b.logger.Info().Int("count", len(bookings)).Msg("Получено заявок из БД")

	if bookings == nil {
		b.logger.Warn().Msg("Bookings is nil")
		b.sendMessage(update.Message.Chat.ID, "Ошибка при получении заявок bookings")
		return
	}

	var message strings.Builder
	message.WriteString("📊 Все заявки на квартал вперед:\n\n")

	for _, booking := range bookings {
		statusEmoji := "⏳"
		switch booking.Status {
		case models.StatusConfirmed:
			statusEmoji = "✅"
		case models.StatusCancelled:
			statusEmoji = "❌"
		case models.StatusChanged:
			statusEmoji = "🔄"
		case "rescheduled":
			statusEmoji = "🔄"
		case models.StatusCompleted:
			statusEmoji = "🏁"
		}

		message.WriteString(fmt.Sprintf("%s Заявка #%d\n", statusEmoji, booking.ID))
		message.WriteString(fmt.Sprintf("   👤 %s\n", booking.UserName))
		message.WriteString(fmt.Sprintf("   🏢 %s\n", booking.ItemName))
		message.WriteString(fmt.Sprintf("   📅 %s\n", booking.Date.Format("02.01.2006")))
		message.WriteString(fmt.Sprintf("   📱 %s\n", booking.Phone))
		message.WriteString(fmt.Sprintf("   🔗 /manager_booking_%d\n\n", booking.ID))
	}

	if len(bookings) == 0 {
		message.WriteString("Заявок не найдено")
	}

	b.sendMessage(update.Message.Chat.ID, message.String())
}

// showManagerBookingDetail показывает детали заявки менеджеру
func (b *Bot) showManagerBookingDetail(ctx context.Context, update tgbotapi.Update, bookingID int64) {
	// ПРОВЕРКА НА NIL - чтобы избежать паники
	if update.Message == nil {
		b.logger.Error().Msg("Error: update.Message is nil in showManagerBookingDetail")
		return
	}

	booking, err := b.db.GetBooking(ctx, bookingID)
	if err != nil {
		b.sendMessage(update.Message.Chat.ID, "Заявка не найдена")
		return
	}

	statusText := map[string]string{
		models.StatusPending:   "⏳ Ожидает подтверждения",
		models.StatusConfirmed: "✅ Подтверждена",
		models.StatusCancelled: "❌ Отменена",
		models.StatusChanged:   "🔄 Изменена",
		models.StatusCompleted: "🏁 Завершена",
	}

	message := fmt.Sprintf(`📋 Заявка #%d

👤 Клиент: %s
📱 Телефон: %s
🏢 Позиция: %s
📅 Дата: %s
📊 Статус: %s
💬 Комментарий: %s
🕐 Создана: %s
✏️ Обновлена: %s`,
		booking.ID,
		booking.UserName,
		booking.Phone,
		booking.ItemName,
		booking.Date.Format("02.01.2006"),
		statusText[booking.Status],
		booking.Comment,
		booking.CreatedAt.Format("02.01.2006 15:04"),
		booking.UpdatedAt.Format("02.01.2006 15:04"),
	)

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, message)

	// Создаем инлайн-клавиатуру для управления заявкой
	var rows [][]tgbotapi.InlineKeyboardButton

	if booking.Status == models.StatusPending || booking.Status == models.StatusChanged {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить", fmt.Sprintf("confirm_%d", booking.ID)),
			tgbotapi.NewInlineKeyboardButtonData("❌ Отклонить", fmt.Sprintf("reject_%d", booking.ID)),
		))
	}

	if booking.Status == models.StatusConfirmed {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 Вернуть в работу", fmt.Sprintf("reopen_%d", booking.ID)),
			tgbotapi.NewInlineKeyboardButtonData("🏁 Завершить", fmt.Sprintf("complete_%d", booking.ID)),
		))
	}

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("✏️ Изменить аппарат", fmt.Sprintf("change_item_%d", booking.ID)),
		tgbotapi.NewInlineKeyboardButtonData("🔄 Предложить выбрать другую дату", fmt.Sprintf("reschedule_%d", booking.ID)),
	))
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("📞 Позвонить", fmt.Sprintf("call_booking:%d", booking.ID)),
	))

	if len(rows) > 0 {
		keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)
		msg.ReplyMarkup = &keyboard
	}

	b.bot.Send(msg)
}

// startChangeItem начало изменения аппарата в заявке
func (b *Bot) startChangeItem(ctx context.Context, booking *models.Booking, managerChatID int64) {
	msg := tgbotapi.NewMessage(managerChatID,
		"Выберите новый аппарат для заявки #"+strconv.FormatInt(booking.ID, 10)+":")

	var keyboardRows [][]tgbotapi.InlineKeyboardButton
	for _, item := range b.items {
		row := tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(item.Name,
				fmt.Sprintf("change_to_%d_%d", booking.ID, item.ID)),
		)
		keyboardRows = append(keyboardRows, row)
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(keyboardRows...)
	msg.ReplyMarkup = &keyboard

	b.bot.Send(msg)
}

// handleChangeItem обработка выбора нового аппарата С ПРОВЕРКОЙ ДОСТУПНОСТИ
func (b *Bot) handleChangeItem(ctx context.Context, update tgbotapi.Update) {
	callback := update.CallbackQuery
	if callback == nil {
		return
	}

	data := callback.Data
	var bookingID, itemID int64

	if _, err := fmt.Sscanf(data, "change_to_%d_%d", &bookingID, &itemID); err != nil {
		return
	}

	// Находим выбранный аппарат
	var selectedItem models.Item
	for _, item := range b.items {
		if item.ID == itemID {
			selectedItem = item
			break
		}
	}

	if selectedItem.ID == 0 {
		b.sendMessage(callback.Message.Chat.ID, "Аппарат не найден")
		return
	}

	// ПРОВЕРЯЕМ ДОСТУПНОСТЬ нового аппарата на дату заявки
	booking, available, err := b.db.GetBookingWithAvailability(ctx, bookingID, selectedItem.ID)
	if err != nil {
		b.logger.Error().Err(err).Int64("booking_id", bookingID).Int64("item_id", selectedItem.ID).Msg("Error checking availability")
		b.sendMessage(callback.Message.Chat.ID, "Ошибка при проверке доступности")
		return
	}

	if !available {
		b.sendMessage(callback.Message.Chat.ID,
			fmt.Sprintf("❌ Аппарат '%s' недоступен на дату %s. Выберите другой аппарат.",
				selectedItem.Name, booking.Date.Format("02.01.2006")))
		return
	}

	// Обновляем заявку и статус с проверкой версии
	err = b.db.UpdateBookingItemAndStatusWithVersion(ctx, bookingID, booking.Version, selectedItem.ID, selectedItem.Name, models.StatusChanged)
	if err != nil {
		if err == database.ErrConcurrentModification {
			b.sendMessage(callback.Message.Chat.ID, "Заявка была обновлена кем-то еще. Обновите данные и попробуйте снова.")
			return
		}
		b.logger.Error().Err(err).Int64("booking_id", bookingID).Msg("Error updating booking item")
		b.sendMessage(callback.Message.Chat.ID, "Ошибка при обновлении заявки")
		return
	}

	booking.ItemID = selectedItem.ID
	booking.ItemName = selectedItem.Name
	booking.Status = models.StatusChanged
	booking.Version++
	b.publishBookingEvent(ctx, events.EventBookingItemChange, *booking, "manager", callback.From.ID)

	// Уведомляем пользователя
	userMsg := tgbotapi.NewMessage(booking.UserID,
		fmt.Sprintf("🔄 В вашей заявке #%d изменен аппарат на: %s", bookingID, selectedItem.Name))
	b.bot.Send(userMsg)

	b.sendMessage(callback.Message.Chat.ID, "✅ Аппарат успешно изменен")

	// Асинхронно обновляем расписание в Google Sheets
	go b.SyncScheduleToSheets(ctx)

	// ВМЕСТО ВЫЗОВА showManagerBookingDetail, который требует Message, используем sendManagerBookingDetail
	updatedBooking, err := b.db.GetBooking(ctx, bookingID)
	if err != nil {
		b.logger.Error().Err(err).Int64("booking_id", bookingID).Msg("Error getting updated booking")
		return
	}

	// Отправляем обновленные детали заявки
	b.sendManagerBookingDetail(ctx, callback.Message.Chat.ID, updatedBooking)
}

// sendManagerBookingDetail отправляет детали заявки в указанный чат (без использования update)
func (b *Bot) sendManagerBookingDetail(ctx context.Context, chatID int64, booking *models.Booking) {
	statusText := map[string]string{
		models.StatusPending:   "⏳ Ожидает подтверждения",
		models.StatusConfirmed: "✅ Подтверждена",
		models.StatusCancelled: "❌ Отменена",
		models.StatusChanged:   "🔄 Изменена",
		models.StatusCompleted: "🏁 Завершена",
	}

	message := fmt.Sprintf(`📋 Заявка #%d

👤 Клиент: %s
📱 Телефон: %s
🏢 Позиция: %s
📅 Дата: %s
📊 Статус: %s
🕐 Создана: %s
✏️ Обновлена: %s`,
		booking.ID,
		booking.UserName,
		booking.Phone,
		booking.ItemName,
		booking.Date.Format("02.01.2006"),
		statusText[booking.Status],
		booking.CreatedAt.Format("02.01.2006 15:04"),
		booking.UpdatedAt.Format("02.01.2006 15:04"),
	)

	msg := tgbotapi.NewMessage(chatID, message)

	// Создаем инлайн-клавиатуру для управления заявкой
	var rows [][]tgbotapi.InlineKeyboardButton

	if booking.Status == models.StatusPending || booking.Status == models.StatusChanged {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить", fmt.Sprintf("confirm_%d", booking.ID)),
			tgbotapi.NewInlineKeyboardButtonData("❌ Отклонить", fmt.Sprintf("reject_%d", booking.ID)),
		))
	}

	if booking.Status == models.StatusConfirmed {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 Вернуть в работу", fmt.Sprintf("reopen_%d", booking.ID)),
			tgbotapi.NewInlineKeyboardButtonData("🏁 Завершить", fmt.Sprintf("complete_%d", booking.ID)),
		))
	}

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("✏️ Изменить аппарат", fmt.Sprintf("change_item_%d", booking.ID)),
		tgbotapi.NewInlineKeyboardButtonData("🔄 Предложить выбрать другую дату", fmt.Sprintf("reschedule_%d", booking.ID)),
	))
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("📞 Позвонить", fmt.Sprintf("call_booking:%d", booking.ID)),
	))

	if len(rows) > 0 {
		keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)
		msg.ReplyMarkup = &keyboard
	}

	b.bot.Send(msg)
}

// reopenBooking возврат заявки в работу
func (b *Bot) reopenBooking(ctx context.Context, booking *models.Booking, managerChatID int64) {
	err := b.db.UpdateBookingStatusWithVersion(ctx, booking.ID, booking.Version, models.StatusPending)
	if err != nil {
		if err == database.ErrConcurrentModification {
			b.sendMessage(managerChatID, "Заявка уже изменена. Обновите данные и попробуйте снова.")
			return
		}
		b.logger.Error().Err(err).Int64("booking_id", booking.ID).Msg("Error reopening booking")
		return
	}

	booking.Version++
	booking.Status = models.StatusPending

	// Уведомляем пользователя
	userMsg := tgbotapi.NewMessage(booking.UserID,
		fmt.Sprintf("🔄 Ваша заявка #%d возвращена в работу. Ожидайте подтверждения.", booking.ID))
	b.bot.Send(userMsg)

	managerMsg := tgbotapi.NewMessage(managerChatID, "✅ Заявка возвращена в работу")
	b.bot.Send(managerMsg)

	// Асинхронно обновляем расписание в Google Sheets
	go b.SyncScheduleToSheets(ctx)
}

func (b *Bot) handleAddItemCommand(ctx context.Context, update tgbotapi.Update) {
	parts := strings.Fields(update.Message.Text)
	if len(parts) < 3 {
		b.sendMessage(update.Message.Chat.ID, "Использование: /add_item <название> <количество>")
		return
	}

	qty, err := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	if err != nil || qty <= 0 {
		b.sendMessage(update.Message.Chat.ID, "Количество должно быть положительным числом")
		return
	}

	name := strings.Join(parts[1:len(parts)-1], " ")
	item := &models.Item{Name: name, TotalQuantity: qty}
	if err := b.db.CreateItem(ctx, item); err != nil {
		b.sendMessage(update.Message.Chat.ID, fmt.Sprintf("Не удалось создать аппарат: %v", err))
		return
	}

	b.refreshItemsFromDB(ctx)
	b.sendMessage(update.Message.Chat.ID, fmt.Sprintf("✅ Аппарат '%s' добавлен (кол-во: %d, порядок: %d)", item.Name, item.TotalQuantity, item.SortOrder))
}

func (b *Bot) handleEditItemCommand(ctx context.Context, update tgbotapi.Update) {
	parts := strings.Fields(update.Message.Text)
	if len(parts) < 3 {
		b.sendMessage(update.Message.Chat.ID, "Использование: /edit_item <название> <новое_количество>")
		return
	}

	qty, err := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	if err != nil || qty <= 0 {
		b.sendMessage(update.Message.Chat.ID, "Количество должно быть положительным числом")
		return
	}

	name := strings.Join(parts[1:len(parts)-1], " ")
	current, err := b.db.GetItemByName(ctx, name)
	if err != nil {
		b.sendMessage(update.Message.Chat.ID, fmt.Sprintf("Аппарат '%s' не найден", name))
		return
	}

	current.TotalQuantity = qty
	if err := b.db.UpdateItem(ctx, current); err != nil {
		b.sendMessage(update.Message.Chat.ID, fmt.Sprintf("Не удалось обновить аппарат: %v", err))
		return
	}

	b.refreshItemsFromDB(ctx)
	b.sendMessage(update.Message.Chat.ID, fmt.Sprintf("✅ Аппарат '%s' обновлён (кол-во: %d)", current.Name, current.TotalQuantity))
}

func (b *Bot) handleListItemsCommand(ctx context.Context, update tgbotapi.Update) {
	items, err := b.db.GetActiveItems(ctx)
	if err != nil {
		b.sendMessage(update.Message.Chat.ID, fmt.Sprintf("Ошибка загрузки списка: %v", err))
		return
	}

	if len(items) == 0 {
		b.sendMessage(update.Message.Chat.ID, "Активные аппараты отсутствуют")
		return
	}

	var sb strings.Builder
	sb.WriteString("📋 Список активных аппаратов:\n")
	for _, it := range items {
		sb.WriteString(fmt.Sprintf("• %s — qty: %d, order: %d\n", it.Name, it.TotalQuantity, it.SortOrder))
	}

	b.sendMessage(update.Message.Chat.ID, sb.String())
}

func (b *Bot) handleDisableItemCommand(ctx context.Context, update tgbotapi.Update) {
	parts := strings.Fields(update.Message.Text)
	if len(parts) < 2 {
		b.sendMessage(update.Message.Chat.ID, "Использование: /disable_item <название>")
		return
	}

	name := strings.Join(parts[1:], " ")
	item, err := b.db.GetItemByName(ctx, name)
	if err != nil {
		b.sendMessage(update.Message.Chat.ID, fmt.Sprintf("Аппарат '%s' не найден", name))
		return
	}

	if err := b.db.DeactivateItem(ctx, item.ID); err != nil {
		b.sendMessage(update.Message.Chat.ID, fmt.Sprintf("Не удалось отключить аппарат: %v", err))
		return
	}

	b.refreshItemsFromDB(ctx)
	b.sendMessage(update.Message.Chat.ID, fmt.Sprintf("🛑 Аппарат '%s' деактивирован", item.Name))
}

func (b *Bot) handleSetItemOrderCommand(ctx context.Context, update tgbotapi.Update) {
	parts := strings.Fields(update.Message.Text)
	if len(parts) < 3 {
		b.sendMessage(update.Message.Chat.ID, "Использование: /set_item_order <название> <порядок>")
		return
	}

	order, err := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	if err != nil || order < 1 {
		b.sendMessage(update.Message.Chat.ID, "Порядок должен быть положительным числом")
		return
	}

	name := strings.Join(parts[1:len(parts)-1], " ")
	item, err := b.db.GetItemByName(ctx, name)
	if err != nil {
		b.sendMessage(update.Message.Chat.ID, fmt.Sprintf("Аппарат '%s' не найден", name))
		return
	}

	if err := b.db.ReorderItem(ctx, item.ID, order); err != nil {
		b.sendMessage(update.Message.Chat.ID, fmt.Sprintf("Не удалось изменить порядок: %v", err))
		return
	}

	b.refreshItemsFromDB(ctx)
	b.sendMessage(update.Message.Chat.ID, fmt.Sprintf("↕️ Порядок '%s' установлен на %d", item.Name, order))
}

func (b *Bot) handleMoveItemCommand(ctx context.Context, update tgbotapi.Update, delta int64) {
	parts := strings.Fields(update.Message.Text)
	if len(parts) < 2 {
		b.sendMessage(update.Message.Chat.ID, "Использование: /move_item_up|/move_item_down <название>")
		return
	}

	name := strings.Join(parts[1:], " ")
	item, err := b.db.GetItemByName(ctx, name)
	if err != nil {
		b.sendMessage(update.Message.Chat.ID, fmt.Sprintf("Аппарат '%s' не найден", name))
		return
	}

	newOrder := item.SortOrder + delta
	if newOrder < 1 {
		newOrder = 1
	}

	if err := b.db.ReorderItem(ctx, item.ID, newOrder); err != nil {
		b.sendMessage(update.Message.Chat.ID, fmt.Sprintf("Не удалось изменить порядок: %v", err))
		return
	}

	b.refreshItemsFromDB(ctx)
	direction := "вверх"
	if delta > 0 {
		direction = "вниз"
	}
	b.sendMessage(update.Message.Chat.ID, fmt.Sprintf("↕️ Аппарат '%s' перемещён %s (новый порядок: %d)", item.Name, direction, newOrder))
}

func (b *Bot) refreshItemsFromDB(ctx context.Context) {
	items, err := b.db.GetActiveItems(ctx)
	if err != nil {
		b.logger.Error().Err(err).Msg("failed to refresh items")
		return
	}
	b.items = items
	b.db.SetItems(items)
}

// completeBooking завершение заявки
func (b *Bot) completeBooking(ctx context.Context, booking *models.Booking, managerChatID int64) {
	err := b.db.UpdateBookingStatusWithVersion(ctx, booking.ID, booking.Version, models.StatusCompleted)
	if err != nil {
		if err == database.ErrConcurrentModification {
			b.sendMessage(managerChatID, "Заявка уже изменена. Обновите данные и попробуйте снова.")
			return
		}
		b.logger.Error().Err(err).Int64("booking_id", booking.ID).Msg("Error completing booking")
		return
	}

	booking.Version++
	booking.Status = models.StatusCompleted

	booking.Status = models.StatusCompleted
	b.publishBookingEvent(ctx, events.EventBookingCompleted, *booking, "manager", managerChatID)

	// Уведомляем пользователя
	userMsg := tgbotapi.NewMessage(booking.UserID,
		fmt.Sprintf("🏁 Ваша заявка #%d завершена. Спасибо за использование наших услуг!", booking.ID))
	b.bot.Send(userMsg)

	managerMsg := tgbotapi.NewMessage(managerChatID, "✅ Заявка завершена")
	b.bot.Send(managerMsg)

	// Асинхронно обновляем расписание в Google Sheets
	go b.SyncScheduleToSheets(ctx)
}

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
	dailyBookings, err := b.db.GetDailyBookings(ctx, startDate, endDate)
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
	for _, item := range b.items {
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

// confirmBooking подтверждение бронирования менеджером
func (b *Bot) confirmBooking(ctx context.Context, booking *models.Booking, managerChatID int64) {
	err := b.db.UpdateBookingStatusWithVersion(ctx, booking.ID, booking.Version, models.StatusConfirmed)
	if err != nil {
		if err == database.ErrConcurrentModification {
			b.sendMessage(managerChatID, "Заявка уже изменена. Обновите данные и попробуйте снова.")
			return
		}
		b.logger.Error().Err(err).Int64("booking_id", booking.ID).Msg("Error confirming booking")
		return
	}

	booking.Version++
	booking.Status = models.StatusConfirmed

	booking.Status = models.StatusConfirmed
	b.publishBookingEvent(ctx, events.EventBookingConfirmed, *booking, "manager", managerChatID)

	// Уведомляем пользователя
	userMsg := tgbotapi.NewMessage(booking.UserID,
		fmt.Sprintf("✅ Ваша заявка на %s %s подтверждена!",
			booking.ItemName, booking.Date.Format("02.01.2006")))
	b.bot.Send(userMsg)

	// Уведомляем менеджера
	managerMsg := tgbotapi.NewMessage(managerChatID, "✅ Бронирование подтверждено")
	b.bot.Send(managerMsg)

	// Асинхронно обновляем расписание в Google Sheets
	go b.SyncScheduleToSheets(ctx)
}

// rejectBooking отклонение бронирования менеджером
func (b *Bot) rejectBooking(ctx context.Context, booking *models.Booking, managerChatID int64) {
	err := b.db.UpdateBookingStatusWithVersion(ctx, booking.ID, booking.Version, models.StatusCancelled)
	if err != nil {
		if err == database.ErrConcurrentModification {
			b.sendMessage(managerChatID, "Заявка уже изменена. Обновите данные и попробуйте снова.")
			return
		}
		b.logger.Error().Err(err).Int64("booking_id", booking.ID).Msg("Error rejecting booking")
		return
	}

	booking.Version++
	booking.Status = models.StatusCancelled

	booking.Status = models.StatusCancelled
	b.publishBookingEvent(ctx, events.EventBookingCancelled, *booking, "manager", managerChatID)

	// Уведомляем пользователя
	userMsg := tgbotapi.NewMessage(booking.UserID,
		"❌ К сожалению, ваша заявка была отклонена менеджером.")
	b.bot.Send(userMsg)

	managerMsg := tgbotapi.NewMessage(managerChatID, "❌ Бронирование отменено")
	b.bot.Send(managerMsg)

	// Асинхронно обновляем расписание в Google Sheets
	go b.SyncScheduleToSheets(ctx)
}

// rescheduleBooking предложение выбрать другую дату
func (b *Bot) rescheduleBooking(ctx context.Context, booking *models.Booking, managerChatID int64) {
	// Отправляем пользователю сообщение с предложением выбрать другую дату
	userMsg := tgbotapi.NewMessage(booking.UserID,
		fmt.Sprintf("🔄 Менеджер предложил выбрать другую дату для %s. Пожалуйста, создайте новую заявку.",
			booking.ItemName))

	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📋 СОЗДАТЬ ЗАЯВКУ"),
		),
	)
	userMsg.ReplyMarkup = keyboard

	b.bot.Send(userMsg)

	// Обновляем статус текущей заявки
	err := b.db.UpdateBookingStatus(ctx, booking.ID, "rescheduled")
	if err != nil {
		b.logger.Error().Err(err).Int64("booking_id", booking.ID).Msg("Error updating booking status")
	}

	managerMsg := tgbotapi.NewMessage(managerChatID, "🔄 Пользователю предложено выбрать другую дату")
	b.bot.Send(managerMsg)

	// Асинхронно обновляем расписание в Google Sheets
	go b.SyncScheduleToSheets(ctx)
}

// notifyManagers уведомление менеджеров о новой заявке
func (b *Bot) notifyManagers(booking models.Booking) {
	message := fmt.Sprintf(`🆕 Новая заявка на бронирование:

🏢 Позиция: %s
📅 Дата: %s
👤 Клиент: %s
📱 Телефон: %s
💬 Комментарий: %s
🆔 ID заявки: %d`,
		booking.ItemName,
		booking.Date.Format("02.01.2006"),
		booking.UserName,
		booking.Phone,
		booking.Comment,
		booking.ID)

	for _, managerID := range b.config.Managers {
		msg := tgbotapi.NewMessage(managerID, message)

		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить", fmt.Sprintf("confirm_%d", booking.ID)),
				tgbotapi.NewInlineKeyboardButtonData("❌ Отклонить", fmt.Sprintf("reject_%d", booking.ID)),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✏️ Изменить аппарат", fmt.Sprintf("change_item_%d", booking.ID)),
				tgbotapi.NewInlineKeyboardButtonData("🔄 Предложить выбрать другую дату", fmt.Sprintf("reschedule_%d", booking.ID)),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("📞 Позвонить", fmt.Sprintf("call_booking:%d", booking.ID)),
			),
		)
		msg.ReplyMarkup = &keyboard

		b.bot.Send(msg)
	}
}

// editManagerItemsPage редактирует страницу с аппаратами для менеджера
func (b *Bot) editManagerItemsPage(update tgbotapi.Update, page int) {
	callback := update.CallbackQuery
	itemsPerPage := 8
	startIdx := page * itemsPerPage
	endIdx := startIdx + itemsPerPage
	if endIdx > len(b.items) {
		endIdx = len(b.items)
	}

	var message strings.Builder
	message.WriteString("🏢 *Выберите аппарат:*\n\n")
	message.WriteString(fmt.Sprintf("Страница %d из %d\n\n", page+1, (len(b.items)+itemsPerPage-1)/itemsPerPage))

	currentItems := b.items[startIdx:endIdx]
	for i, item := range currentItems {
		message.WriteString(fmt.Sprintf("%d. *%s*\n", startIdx+i+1, item.Name))
		message.WriteString(fmt.Sprintf("   📝 %s\n", item.Description))
		message.WriteString(fmt.Sprintf("   👥 Вместимость: %d чел.\n\n", item.TotalQuantity))
	}

	var keyboard [][]tgbotapi.InlineKeyboardButton

	for i, item := range currentItems {
		btn := tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("%d. %s", startIdx+i+1, item.Name),
			fmt.Sprintf("manager_select_item:%d", item.ID),
		)
		keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{btn})
	}

	var navButtons []tgbotapi.InlineKeyboardButton

	if page > 0 {
		navButtons = append(navButtons, tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", fmt.Sprintf("manager_items_page:%d", page-1)))
	}

	if endIdx < len(b.items) {
		navButtons = append(navButtons, tgbotapi.NewInlineKeyboardButtonData("Вперед ➡️", fmt.Sprintf("manager_items_page:%d", page+1)))
	}

	if len(navButtons) > 0 {
		keyboard = append(keyboard, navButtons)
	}

	markup := tgbotapi.NewInlineKeyboardMarkup(keyboard...)

	editMsg := tgbotapi.NewEditMessageTextAndMarkup(
		callback.Message.Chat.ID,
		callback.Message.MessageID,
		message.String(),
		markup,
	)
	editMsg.ParseMode = "Markdown"

	b.bot.Send(editMsg)
	b.bot.Send(tgbotapi.NewCallback(callback.ID, ""))
}

// handleCallButton обработка нажатия кнопки "Позвонить"
func (b *Bot) handleCallButton(ctx context.Context, update tgbotapi.Update) {
	callback := update.CallbackQuery
	if callback == nil {
		return
	}

	// Извлекаем ID заявки из callback data
	data := strings.TrimPrefix(callback.Data, "call_booking:")

	// Парсим ID заявки
	bookingID, err := strconv.ParseInt(data, 10, 64)
	if err != nil {
		b.sendMessage(callback.Message.Chat.ID, "❌ Ошибка: неверный формат данных заявки")
		// Подтверждаем callback даже при ошибке
		b.bot.Send(tgbotapi.NewCallback(callback.ID, "❌ Ошибка"))
		return
	}

	// Получаем заявку из базы данных
	booking, err := b.db.GetBooking(ctx, bookingID)
	if err != nil {
		b.sendMessage(callback.Message.Chat.ID, "❌ Заявка не найдена")
		b.bot.Send(tgbotapi.NewCallback(callback.ID, "❌ Заявка не найдена"))
		return
	}

	if booking.Phone == "" {
		b.sendMessage(callback.Message.Chat.ID, "❌ Номер телефона не указан в заявке")
		b.bot.Send(tgbotapi.NewCallback(callback.ID, "❌ Номер не указан"))
		return
	}

	// Форматируем номер для отображения
	formattedPhone := b.formatPhoneForDisplay(booking.Phone)

	// Создаем информативное сообщение
	message := fmt.Sprintf("📞 *Информация для связи*\n\n")
	message += fmt.Sprintf("👤 *Клиент:* %s\n", booking.UserName)
	message += fmt.Sprintf("📱 *Телефон:* `%s`\n", formattedPhone)
	message += fmt.Sprintf("🏢 *Аппарат:* %s\n", booking.ItemName)
	message += fmt.Sprintf("📅 *Дата:* %s\n", booking.Date.Format("02.01.2006"))

	if booking.Comment != "" {
		message += fmt.Sprintf("💬 *Комментарий:* %s\n", booking.Comment)
	}

	msg := tgbotapi.NewMessage(callback.Message.Chat.ID, message)
	msg.ParseMode = "Markdown"

	// Создаем клавиатуру с быстрыми действиями
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("💬 WhatsApp", fmt.Sprintf("https://wa.me/%s", strings.TrimPrefix(booking.Phone, "+"))),
			tgbotapi.NewInlineKeyboardButtonURL("✉️ Telegram", fmt.Sprintf("https://t.me/%s", strings.TrimPrefix(booking.Phone, "+"))),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад к заявке", fmt.Sprintf("show_booking:%d", booking.ID)),
		),
	)
	msg.ReplyMarkup = &keyboard

	b.bot.Send(tgbotapi.NewCallback(callback.ID, "✅"))
	b.bot.Send(msg)
}

// getUserStats показывает статистику менеджеру
func (b *Bot) getUserStats(ctx context.Context, update tgbotapi.Update) {
if !b.isManager(update.Message.From.ID) {
return
}

allUsers, err := b.db.GetAllUsers(ctx)
if err != nil {
b.logger.Error().Err(err).Msg("Error getting all users")
b.sendMessage(update.Message.Chat.ID, "Ошибка при получении данных")
return
}

activeUsers, _ := b.db.GetActiveUsers(ctx, 30)
managers, _ := b.db.GetUsersByManagerStatus(ctx, true)

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
msg.ParseMode = "Markdown"

keyboard := tgbotapi.NewInlineKeyboardMarkup(
tgbotapi.NewInlineKeyboardRow(
tgbotapi.NewInlineKeyboardButtonData("📤 Экспорт пользователей", "export_users"),
),
)
msg.ReplyMarkup = &keyboard

b.bot.Send(msg)
}

// bookingSummary агрегирует заявки за период в компактный блок: всего, статусы, топ-товары.
func (b *Bot) bookingSummary(ctx context.Context, startDate, endDate time.Time) string {
bookings, err := b.db.GetBookingsByDateRange(ctx, startDate, endDate)
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

statusOrder := []string{models.StatusPending, models.StatusConfirmed, models.StatusChanged, models.StatusCompleted, models.StatusCancelled}
var statusParts []string
for _, st := range statusOrder {
if c := statusCount[st]; c > 0 {
statusParts = append(statusParts, fmt.Sprintf("%s:%d", st, c))
}
}

type kv struct {
name  string
count int
}
var items []kv
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
var itemParts []string
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
func (b *Bot) handleExportUsers(ctx context.Context, update tgbotapi.Update) {
callback := update.CallbackQuery
if callback == nil || !b.isManager(callback.From.ID) {
return
}

users, err := b.db.GetAllUsers(ctx)
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

_, err = b.bot.Send(doc)
if err != nil {
b.logger.Error().Err(err).Msg("Error sending document")
b.sendMessage(callback.Message.Chat.ID, "Ошибка при отправке файла")
return
}

b.sendMessage(callback.Message.Chat.ID, "✅ Файл с пользователями успешно отправлен")
}
