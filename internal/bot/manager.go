package bot

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"bronivik/internal/models"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// handleManagerCommand обработка команд менеджера
func (b *Bot) handleManagerCommand(update tgbotapi.Update) bool {
	if !b.isManager(update.Message.From.ID) {
		return false
	}

	userID := update.Message.From.ID
	text := update.Message.Text
	state := b.getUserState(userID)

	switch {
	case text == "👨‍💼 Все заявки":
		b.showManagerBookings(update)

	case text == "/get_all":
		b.showManagerBookings(update)

	case text == "➕ Создать заявку (Менеджер)":
		b.startManagerBooking(update)

	// секретная команда, доступная менеджерам, но не отображаемся у них в меню
	case text == "/stats" && b.isManager(userID):
		b.getUserStats(update)

	case strings.HasPrefix(text, "/manager_booking_"):
		// Просмотр конкретной заявки
		parts := strings.Split(text, "_")
		if len(parts) >= 3 {
			bookingID, err := strconv.ParseInt(parts[2], 10, 64)
			if err == nil {
				b.showManagerBookingDetail(update, bookingID)
			}
		}

	case state != nil && state.CurrentStep == "manager_waiting_client_name":
		b.handleManagerClientName(update, text, state)

	case state != nil && state.CurrentStep == "manager_waiting_client_phone":
		b.handleManagerClientPhone(update, text, state)

	case state != nil && state.CurrentStep == "manager_waiting_single_date":
		b.handleManagerSingleDate(update, text, state)

	case state != nil && state.CurrentStep == "manager_waiting_start_date":
		b.handleManagerStartDate(update, text, state)

	case state != nil && state.CurrentStep == "manager_waiting_end_date":
		b.handleManagerEndDate(update, text, state)

	case state != nil && state.CurrentStep == "manager_waiting_comment":
		b.handleManagerComment(update, text, state)

	case state != nil && state.CurrentStep == "manager_confirm_booking" && text == "✅ Подтвердить создание":
		b.createManagerBookings(update, state)

	case state != nil && state.CurrentStep == "manager_confirm_booking" && text == "❌ Отмена":
		b.clearUserState(update.Message.From.ID)
		b.sendMessage(update.Message.Chat.ID, "❌ Создание заявки отменено")
		b.handleMainMenu(update)

	case text == "🔄 Синхронизировать бронирования (Google Sheets)":
		b.SyncBookingsToSheets()
		b.sendMessage(update.Message.Chat.ID, "✅ Бронирования синхронизированы с Google Таблицей")

	case text == "📅 Синхронизировать расписание (Google Sheets)":
		b.SyncScheduleToSheets()
		b.sendMessage(update.Message.Chat.ID, "✅ Расписание синхронизировано с Google Таблицей")
	}

	return false
}

// handleManagerAction обработка действий менеджера с заявками
func (b *Bot) handleManagerAction(update tgbotapi.Update) {
	callback := update.CallbackQuery
	if callback == nil {
		return
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
		return
	}

	booking, err := b.db.GetBooking(context.Background(), bookingID)
	if err != nil {
		log.Printf("Error getting booking: %v", err)
		return
	}

	switch action {
	case "confirm_":
		b.confirmBooking(booking, callback.Message.Chat.ID)
	case "reject_":
		b.rejectBooking(booking, callback.Message.Chat.ID)
	case "reschedule_":
		b.rescheduleBooking(booking, callback.Message.Chat.ID)
	case "change_item_":
		b.startChangeItem(booking, callback.Message.Chat.ID)
	case "reopen_":
		b.reopenBooking(booking, callback.Message.Chat.ID)
	case "complete_":
		b.completeBooking(booking, callback.Message.Chat.ID)
	}

	// Обновляем сообщение у менеджера
	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID,
		fmt.Sprintf("✅ Заявка #%d обработана\nДействие: %s", bookingID, action))
	b.bot.Send(editMsg)

	// СИНХРОНИЗИРУЕМ ВСЕ ИЗМЕНЕНИЯ
	go func() {
		time.Sleep(1 * time.Second) // Небольшая задержка для завершения операции в БД
		b.SyncBookingsToSheets()
		b.SyncScheduleToSheets()
	}()
}

// startManagerBooking начало создания заявки менеджером
func (b *Bot) startManagerBooking(update tgbotapi.Update) {
	if !b.isManager(update.Message.From.ID) {
		return
	}

	msg := tgbotapi.NewMessage(update.Message.Chat.ID,
		"📋 Создание заявки от имени клиента\n\nВведите Имя клиента:")

	b.setUserState(update.Message.From.ID, "manager_waiting_client_name", map[string]interface{}{
		"is_manager_booking": true,
	})
	b.bot.Send(msg)
}

// handleManagerClientName обработка ввода имени клиента
func (b *Bot) handleManagerClientName(update tgbotapi.Update, text string, state *models.UserState) {
	state.TempData["client_name"] = text
	b.setUserState(update.Message.From.ID, "manager_waiting_client_phone", state.TempData)

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, "📱 Введите телефон клиента:")
	b.bot.Send(msg)
}

// handleManagerClientPhone обработка ввода телефона клиента
func (b *Bot) handleManagerClientPhone(update tgbotapi.Update, text string, state *models.UserState) {
	// Нормализуем телефон
	normalizedPhone := b.normalizePhone(text)
	if normalizedPhone == "" {
		b.sendMessage(update.Message.Chat.ID, "Неверный формат номера телефона. Пожалуйста, введите номер в формате +7XXXXXXXXXX или 8XXXXXXXXXX")
		return
	}

	state.TempData["client_phone"] = normalizedPhone
	b.setUserState(update.Message.From.ID, "manager_waiting_item_selection", state.TempData)

	// Показываем выбор аппарата с пагинацией
	b.sendManagerItemsPage(update.Message.Chat.ID, update.Message.From.ID, 0)
}

// sendManagerItemsPage отправляет страницу с аппаратами для менеджера
func (b *Bot) sendManagerItemsPage(chatID, userID int64, page int) {
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
func (b *Bot) handleManagerItemSelection(update tgbotapi.Update) {
	callback := update.CallbackQuery
	data := callback.Data

	itemIDStr := strings.TrimPrefix(data, "manager_select_item:")
	itemID, err := strconv.ParseInt(itemIDStr, 10, 64)
	if err != nil {
		log.Printf("Error parsing item ID: %v", err)
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

	state := b.getUserState(callback.From.ID)
	if state == nil {
		b.sendMessage(callback.Message.Chat.ID, "Сессия устарела. Начните заново.")
		return
	}

	state.TempData["selected_item"] = selectedItem
	b.setUserState(callback.From.ID, "manager_waiting_date_type", state.TempData)

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
func (b *Bot) handleManagerDateType(update tgbotapi.Update, dateType string) {
	callback := update.CallbackQuery
	state := b.getUserState(callback.From.ID)
	if state == nil {
		return
	}

	if dateType == "single" {
		state.TempData["date_type"] = "single"
		b.setUserState(callback.From.ID, "manager_waiting_single_date", state.TempData)

		editMsg := tgbotapi.NewEditMessageText(
			callback.Message.Chat.ID,
			callback.Message.MessageID,
			"📅 Введите дату бронирования в формате ДД.ММ.ГГГГ (например, 25.12.2024):",
		)
		b.bot.Send(editMsg)
	} else {
		state.TempData["date_type"] = "range"
		b.setUserState(callback.From.ID, "manager_waiting_start_date", state.TempData)

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
func (b *Bot) handleManagerSingleDate(update tgbotapi.Update, dateStr string, state *models.UserState) {
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
	b.setUserState(update.Message.From.ID, "manager_waiting_comment", state.TempData)

	b.sendMessage(update.Message.Chat.ID, "💬 Введите комментарий к заявке (например: 'Техническое обслуживание', 'Обучение персонала' или любой другой текст):")
}

// handleManagerStartDate обработка ввода начальной даты интервала
func (b *Bot) handleManagerStartDate(update tgbotapi.Update, dateStr string, state *models.UserState) {
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
	b.setUserState(update.Message.From.ID, "manager_waiting_end_date", state.TempData)

	b.sendMessage(update.Message.Chat.ID, "📅 Введите конечную дату интервала в формате ДД.ММ.ГГГГ:")
}

// handleManagerEndDate обработка ввода конечной даты интервала
func (b *Bot) handleManagerEndDate(update tgbotapi.Update, dateStr string, state *models.UserState) {
	endDate, err := time.Parse("02.01.2006", dateStr)
	if err != nil {
		b.sendMessage(update.Message.Chat.ID, "Неверный формат даты. Используйте ДД.ММ.ГГГГ (например, 25.12.2024)")
		return
	}

	startDate := state.TempData["start_date"].(time.Time)

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
	b.setUserState(update.Message.From.ID, "manager_waiting_comment", state.TempData)

	b.sendMessage(update.Message.Chat.ID, fmt.Sprintf("💬 Введите комментарий к заявке (будет применен ко всем %d дням):", len(dates)))
}

// handleManagerComment обработка ввода комментария
func (b *Bot) handleManagerComment(update tgbotapi.Update, comment string, state *models.UserState) {
	state.TempData["comment"] = comment
	b.setUserState(update.Message.From.ID, "manager_confirm_booking", state.TempData)

	// Показываем подтверждение
	b.showManagerBookingConfirmation(update, state)
}

// showManagerBookingConfirmation показывает подтверждение заявки менеджером
func (b *Bot) showManagerBookingConfirmation(update tgbotapi.Update, state *models.UserState) {
	clientName := state.TempData["client_name"].(string)
	clientPhone := state.TempData["client_phone"].(string)
	selectedItem := state.TempData["selected_item"].(models.Item)
	dates := state.TempData["dates"].([]time.Time)
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
func (b *Bot) createManagerBookings(update tgbotapi.Update, state *models.UserState) {
	clientName := state.TempData["client_name"].(string)
	clientPhone := state.TempData["client_phone"].(string)
	selectedItem := state.TempData["selected_item"].(models.Item)
	dates := state.TempData["dates"].([]time.Time)
	comment := state.TempData["comment"].(string)

	var createdBookings []*models.Booking
	var failedDates []string

	// Создаем заявки на каждую дату
	for _, date := range dates {
		// Проверяем доступность
		available, err := b.db.CheckAvailability(context.Background(), selectedItem.ID, date)
		if err != nil {
			log.Printf("Error checking availability: %v", err)
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
			Status:       "confirmed", // Менеджер создает сразу подтвержденные заявки
			Comment:      comment,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}

		err = b.db.CreateBooking(context.Background(), booking)
		if err != nil {
			log.Printf("Error creating manager booking: %v", err)
			failedDates = append(failedDates, date.Format("02.01.2006"))
		} else {
			createdBookings = append(createdBookings, booking)
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
	b.clearUserState(update.Message.From.ID)

	// СИНХРОНИЗИРУЕМ ВСЕ ИЗМЕНЕНИЯ В GOOGLE SHEETS
	if len(createdBookings) > 0 {
		// СИНХРОНИЗИРУЕМ ВСЕ ИЗМЕНЕНИЯ
		go func() {
			time.Sleep(1 * time.Second) // Небольшая задержка для завершения операции в БД
			b.SyncBookingsToSheets()
			b.SyncScheduleToSheets()
		}()
	}

	// Возвращаем в главное меню
	b.handleMainMenu(update)
}

// showManagerBookings показывает все заявки менеджеру
func (b *Bot) showManagerBookings(update tgbotapi.Update) {
	if !b.isManager(update.Message.From.ID) {
		return
	}

	// Получаем все заявки за период: один месяц назад и два месяца вперед
	startDate := time.Now().AddDate(0, 0, -7) // 7 дней месяц назад
	endDate := time.Now().AddDate(0, 2, 0)    // 2 месяца вперед

	bookings, err := b.db.GetBookingsByDateRange(context.Background(), startDate, endDate)
	if err != nil {
		log.Printf("Error getting bookings: %v", err)
		b.sendMessage(update.Message.Chat.ID, "Ошибка при получении заявок")
		return
	}

	log.Printf("Получено %d заявок из БД", len(bookings))

	if bookings == nil {
		log.Printf("Bookings is nil")
		b.sendMessage(update.Message.Chat.ID, "Ошибка при получении заявок bookings")
		return
	}

	var message strings.Builder
	message.WriteString("📊 Все заявки на квартал вперед:\n\n")

	for _, booking := range bookings {
		statusEmoji := "⏳"
		switch booking.Status {
		case "confirmed":
			statusEmoji = "✅"
		case "cancelled":
			statusEmoji = "❌"
		case "changed":
			statusEmoji = "🔄"
		case "rescheduled":
			statusEmoji = "🔄"
		case "completed":
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
func (b *Bot) showManagerBookingDetail(update tgbotapi.Update, bookingID int64) {
	// ПРОВЕРКА НА NIL - чтобы избежать паники
	if update.Message == nil {
		log.Printf("Error: update.Message is nil in showManagerBookingDetail")
		return
	}

	booking, err := b.db.GetBooking(context.Background(), bookingID)
	if err != nil {
		b.sendMessage(update.Message.Chat.ID, "Заявка не найдена")
		return
	}

	statusText := map[string]string{
		"pending":   "⏳ Ожидает подтверждения",
		"confirmed": "✅ Подтверждена",
		"cancelled": "❌ Отменена",
		"changed":   "🔄 Изменена",
		"completed": "🏁 Завершена",
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

	if booking.Status == "pending" || booking.Status == "changed" || booking.Status == "rescheduled" {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить", fmt.Sprintf("confirm_%d", booking.ID)),
			tgbotapi.NewInlineKeyboardButtonData("❌ Отклонить", fmt.Sprintf("reject_%d", booking.ID)),
		))
	}

	if booking.Status == "confirmed" || booking.Status == "cancelled" {
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
func (b *Bot) startChangeItem(booking *models.Booking, managerChatID int64) {
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
func (b *Bot) handleChangeItem(update tgbotapi.Update) {
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
	booking, available, err := b.db.GetBookingWithAvailability(context.Background(), bookingID, selectedItem.ID)
	if err != nil {
		log.Printf("Error checking availability: %v", err)
		b.sendMessage(callback.Message.Chat.ID, "Ошибка при проверке доступности")
		return
	}

	if !available {
		b.sendMessage(callback.Message.Chat.ID,
			fmt.Sprintf("❌ Аппарат '%s' недоступен на дату %s. Выберите другой аппарат.",
				selectedItem.Name, booking.Date.Format("02.01.2006")))
		return
	}

	// Обновляем заявку
	err = b.db.UpdateBookingItem(context.Background(), bookingID, selectedItem.ID, selectedItem.Name)
	if err != nil {
		log.Printf("Error updating booking item: %v", err)
		b.sendMessage(callback.Message.Chat.ID, "Ошибка при обновлении заявки")
		return
	}

	// Обновляем статус
	err = b.db.UpdateBookingStatus(context.Background(), bookingID, "changed")
	if err != nil {
		log.Printf("Error updating booking status: %v", err)
	}

	// Уведомляем пользователя
	userMsg := tgbotapi.NewMessage(booking.UserID,
		fmt.Sprintf("🔄 В вашей заявке #%d изменен аппарат на: %s", bookingID, selectedItem.Name))
	b.bot.Send(userMsg)

	b.sendMessage(callback.Message.Chat.ID, "✅ Аппарат успешно изменен")

	// СИНХРОНИЗИРУЕМ ИЗМЕНЕНИЯ В GOOGLE SHEETS
	b.SyncBookingsToSheets()
	b.SyncScheduleToSheets()

	// ВМЕСТО ВЫЗОВА showManagerBookingDetail, который требует Message, используем sendManagerBookingDetail
	updatedBooking, err := b.db.GetBooking(context.Background(), bookingID)
	if err != nil {
		log.Printf("Error getting updated booking: %v", err)
		return
	}

	// Отправляем обновленные детали заявки
	b.sendManagerBookingDetail(callback.Message.Chat.ID, updatedBooking)
}

// sendManagerBookingDetail отправляет детали заявки в указанный чат (без использования update)
func (b *Bot) sendManagerBookingDetail(chatID int64, booking *models.Booking) {
	statusText := map[string]string{
		"pending":   "⏳ Ожидает подтверждения",
		"confirmed": "✅ Подтверждена",
		"cancelled": "❌ Отменена",
		"changed":   "🔄 Изменена",
		"completed": "🏁 Завершена",
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

	if booking.Status == "pending" || booking.Status == "changed" {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить", fmt.Sprintf("confirm_%d", booking.ID)),
			tgbotapi.NewInlineKeyboardButtonData("❌ Отклонить", fmt.Sprintf("reject_%d", booking.ID)),
		))
	}

	if booking.Status == "confirmed" {
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
func (b *Bot) reopenBooking(booking *models.Booking, managerChatID int64) {
	err := b.db.UpdateBookingStatus(context.Background(), booking.ID, "pending")
	if err != nil {
		log.Printf("Error reopening booking: %v", err)
		return
	}

	// Уведомляем пользователя
	userMsg := tgbotapi.NewMessage(booking.UserID,
		fmt.Sprintf("🔄 Ваша заявка #%d возвращена в работу. Ожидайте подтверждения.", booking.ID))
	b.bot.Send(userMsg)

	managerMsg := tgbotapi.NewMessage(managerChatID, "✅ Заявка возвращена в работу")
	b.bot.Send(managerMsg)

	// СИНХРОНИЗИРУЕМ ИЗМЕНЕНИЯ В GOOGLE SHEETS
	b.SyncBookingsToSheets()
	b.SyncScheduleToSheets()
}

// completeBooking завершение заявки
func (b *Bot) completeBooking(booking *models.Booking, managerChatID int64) {
	err := b.db.UpdateBookingStatus(context.Background(), booking.ID, "completed")
	if err != nil {
		log.Printf("Error completing booking: %v", err)
		return
	}

	// Уведомляем пользователя
	userMsg := tgbotapi.NewMessage(booking.UserID,
		fmt.Sprintf("🏁 Ваша заявка #%d завершена. Спасибо за использование наших услуг!", booking.ID))
	b.bot.Send(userMsg)

	managerMsg := tgbotapi.NewMessage(managerChatID, "✅ Заявка завершена")
	b.bot.Send(managerMsg)

	// СИНХРОНИЗИРУЕМ ИЗМЕНЕНИЯ В GOOGLE SHEETS
	b.SyncBookingsToSheets()
	b.SyncScheduleToSheets()
}

// SyncScheduleToSheets синхронизирует расписание в формате таблицы с Google Sheets
func (b *Bot) SyncScheduleToSheets() {
	if b.sheetsService == nil {
		log.Println("Google Sheets service not initialized")
		return
	}

	// Определяем период: один месяц назад и два месяца вперед
	startDate := time.Now().AddDate(0, -1, 0).Truncate(24 * time.Hour)
	endDate := time.Now().AddDate(0, 2, 0).Truncate(24 * time.Hour)

	log.Printf("Syncing schedule to Google Sheets from %s to %s",
		startDate.Format("02.01.2006"),
		endDate.Format("02.01.2006"))

	// Получаем данные о бронированиях
	dailyBookings, err := b.db.GetDailyBookings(context.Background(), startDate, endDate)
	if err != nil {
		log.Printf("Failed to get daily bookings for schedule sync: %v", err)
		return
	}

	// Логируем количество найденных бронирований
	totalBookings := 0
	for _, bookings := range dailyBookings {
		totalBookings += len(bookings)
	}
	log.Printf("Found %d bookings across %d dates", totalBookings, len(dailyBookings))

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

	log.Printf("Updating Google Sheets with %d items", len(googleItems))

	// Обновляем расписание в Google Sheets
	err = b.sheetsService.UpdateScheduleSheet(startDate, endDate, googleDailyBookings, googleItems)
	if err != nil {
		log.Printf("Failed to sync schedule to Google Sheets: %v", err)
	} else {
		log.Printf("Schedule successfully synced to Google Sheets")
	}
}

// confirmBooking подтверждение бронирования менеджером
func (b *Bot) confirmBooking(booking *models.Booking, managerChatID int64) {
	err := b.db.UpdateBookingStatus(context.Background(), booking.ID, "confirmed")
	if err != nil {
		log.Printf("Error confirming booking: %v", err)
		return
	}

	// Уведомляем пользователя
	userMsg := tgbotapi.NewMessage(booking.UserID,
		fmt.Sprintf("✅ Ваша заявка на %s %s подтверждена!",
			booking.ItemName, booking.Date.Format("02.01.2006")))
	b.bot.Send(userMsg)

	// Уведомляем менеджера
	managerMsg := tgbotapi.NewMessage(managerChatID, "✅ Бронирование подтверждено")
	b.bot.Send(managerMsg)

	// СИНХРОНИЗИРУЕМ ИЗМЕНЕНИЯ В GOOGLE SHEETS
	b.SyncBookingsToSheets()
	b.SyncScheduleToSheets()
}

// rejectBooking отклонение бронирования менеджером
func (b *Bot) rejectBooking(booking *models.Booking, managerChatID int64) {
	err := b.db.UpdateBookingStatus(context.Background(), booking.ID, "cancelled")
	if err != nil {
		log.Printf("Error rejecting booking: %v", err)
		return
	}

	// Уведомляем пользователя
	userMsg := tgbotapi.NewMessage(booking.UserID,
		"❌ К сожалению, ваша заявка была отклонена менеджером.")
	b.bot.Send(userMsg)

	managerMsg := tgbotapi.NewMessage(managerChatID, "❌ Бронирование отменено")
	b.bot.Send(managerMsg)

	// СИНХРОНИЗИРУЕМ ИЗМЕНЕНИЯ В GOOGLE SHEETS
	b.SyncBookingsToSheets()
	b.SyncScheduleToSheets()
}

// rescheduleBooking предложение выбрать другую дату
func (b *Bot) rescheduleBooking(booking *models.Booking, managerChatID int64) {
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
	err := b.db.UpdateBookingStatus(context.Background(), booking.ID, "rescheduled")
	if err != nil {
		log.Printf("Error updating booking status: %v", err)
	}

	managerMsg := tgbotapi.NewMessage(managerChatID, "🔄 Пользователю предложено выбрать другую дату")
	b.bot.Send(managerMsg)

	// СИНХРОНИЗИРУЕМ ИЗМЕНЕНИЯ В GOOGLE SHEETS
	b.SyncBookingsToSheets()
	b.SyncScheduleToSheets()
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
func (b *Bot) handleCallButton(update tgbotapi.Update) {
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
	booking, err := b.db.GetBooking(context.Background(), bookingID)
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

// formatPhoneForDisplay форматирует номер телефона для красивого отображения
func (b *Bot) formatPhoneForDisplay(phone string) string {
	// Убираем все нецифровые символы
	cleaned := ""
	for _, char := range phone {
		if char >= '0' && char <= '9' {
			cleaned += string(char)
		}
	}

	// Форматируем в зависимости от длины
	if len(cleaned) == 11 && cleaned[0] == '7' {
		// Российский номер: +7 (XXX) XXX-XX-XX
		return fmt.Sprintf("+7 (%s) %s-%s-%s",
			cleaned[1:4], cleaned[4:7], cleaned[7:9], cleaned[9:])
	} else if len(cleaned) == 10 {
		// Номер без кода страны: (XXX) XXX-XX-XX
		return fmt.Sprintf("(%s) %s-%s-%s",
			cleaned[0:3], cleaned[3:6], cleaned[6:8], cleaned[8:])
	}

	// Возвращаем исходный номер, если форматирование не применимо
	return phone
}
