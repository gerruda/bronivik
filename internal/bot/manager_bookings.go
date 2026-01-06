package bot

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"bronivik/internal/database"
	"bronivik/internal/models"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// startManagerBooking начало создания заявки менеджером
func (b *Bot) startManagerBooking(ctx context.Context, update *tgbotapi.Update) {
	if !b.isManager(update.Message.From.ID) {
		return
	}

	msg := tgbotapi.NewMessage(update.Message.Chat.ID,
		"📋 Создание заявки от имени клиента\n\nВведите Имя клиента:")

	b.setUserState(ctx, update.Message.From.ID, models.StateManagerWaitingClientName, map[string]interface{}{
		"is_manager_booking": true,
	})
	if _, err := b.tgService.Send(msg); err != nil {
		b.logger.Error().Err(err).Msg("Failed to send message in startManagerBooking")
	}
}

// handleManagerClientName обработка ввода имени клиента
func (b *Bot) handleManagerClientName(ctx context.Context, update *tgbotapi.Update, text string, state *models.UserState) {
	state.TempData["client_name"] = b.sanitizeInput(text)
	b.setUserState(ctx, update.Message.From.ID, models.StateManagerWaitingClientPhone, state.TempData)

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, "📱 Введите телефон клиента:")
	if _, err := b.tgService.Send(msg); err != nil {
		b.logger.Error().Err(err).Msg("Failed to send message in handleManagerClientName")
	}
}

// handleManagerClientPhone обработка ввода телефона клиента
func (b *Bot) handleManagerClientPhone(ctx context.Context, update *tgbotapi.Update, text string, state *models.UserState) {
	// Нормализуем телефон
	normalizedPhone := b.normalizePhone(text)
	if normalizedPhone == "" {
		b.sendMessage(update.Message.Chat.ID, "Неверный формат номера телефона. Пожалуйста, введите номер в формате +7XXXXXXXXXX или 8XXXXXXXXXX")
		return
	}

	state.TempData["client_phone"] = normalizedPhone
	b.setUserState(ctx, update.Message.From.ID, models.StateManagerWaitingItemSelection, state.TempData)

	// Показываем выбор аппарата с пагинацией
	b.sendManagerItemsPage(ctx, update.Message.Chat.ID, 0, 0)
}

// sendManagerItemsPage отправляет страницу с аппаратами для менеджера
func (b *Bot) sendManagerItemsPage(ctx context.Context, chatID int64, messageID, page int) {
	b.renderPaginatedItems(&PaginationParams{
		Ctx:          ctx,
		ChatID:       chatID,
		MessageID:    messageID,
		Page:         page,
		Title:        "🏢 *Выберите аппарат:*",
		ItemPrefix:   "manager_select_item:",
		PagePrefix:   "manager_items_page:",
		BackCallback: "",
		ShowCapacity: true,
	})
}

// handleManagerItemSelection обработка выбора аппарата менеджером
func (b *Bot) handleManagerItemSelection(ctx context.Context, update *tgbotapi.Update) {
	callback := update.CallbackQuery
	data := callback.Data

	itemIDStr := strings.TrimPrefix(data, "manager_select_item:")
	itemID, err := strconv.ParseInt(itemIDStr, 10, 64)
	if err != nil {
		b.logger.Error().Err(err).Str("item_id_str", itemIDStr).Msg("Error parsing item ID")
		return
	}

	selectedItem, ok := b.getItemByID(itemID)

	if !ok {
		b.sendMessage(callback.Message.Chat.ID, "Аппарат не найден")
		return
	}

	state := b.getUserState(ctx, callback.From.ID)
	if state == nil {
		b.sendMessage(callback.Message.Chat.ID, "Сессия устарела. Начните заново.")
		return
	}

	state.TempData["item_id"] = selectedItem.ID
	b.setUserState(ctx, callback.From.ID, models.StateManagerWaitingDateType, state.TempData)

	// Спрашиваем тип даты (одна дата или интервал)
	msg := tgbotapi.NewMessage(callback.Message.Chat.ID, "📅 Выберите тип бронирования:")

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📅 Одна дата", "manager_single_date"),
			tgbotapi.NewInlineKeyboardButtonData("📆 Интервал дат", "manager_date_range"),
		),
	)
	msg.ReplyMarkup = &keyboard

	if _, err := b.tgService.Send(msg); err != nil {
		b.logger.Error().Err(err).Msg("Failed to send message in handleManagerItemSelection")
	}
	if _, err := b.tgService.Send(tgbotapi.NewCallback(callback.ID, "")); err != nil {
		b.logger.Error().Err(err).Msg("Failed to send callback in handleManagerItemSelection")
	}
}

// handleManagerDateType обработка выбора типа даты
func (b *Bot) handleManagerDateType(ctx context.Context, update *tgbotapi.Update, dateType string) {
	callback := update.CallbackQuery
	state := b.getUserState(ctx, callback.From.ID)
	if state == nil {
		return
	}

	if dateType == typeSingle {
		state.TempData["date_type"] = typeSingle
		b.setUserState(ctx, callback.From.ID, models.StateManagerWaitingSingleDate, state.TempData)

		editMsg := tgbotapi.NewEditMessageText(
			callback.Message.Chat.ID,
			callback.Message.MessageID,
			"📅 Введите дату бронирования в формате ДД.ММ.ГГГГ (например, 25.12.2024):",
		)
		if _, err := b.tgService.Send(editMsg); err != nil {
			b.logger.Error().Err(err).Msg("Failed to send edit message in handleManagerDateType")
		}
	} else {
		state.TempData["date_type"] = "range"
		b.setUserState(ctx, callback.From.ID, models.StateManagerWaitingStartDate, state.TempData)

		editMsg := tgbotapi.NewEditMessageText(
			callback.Message.Chat.ID,
			callback.Message.MessageID,
			"📅 Введите начальную дату интервала в формате ДД.ММ.ГГГГ (например, 25.12.2024):",
		)
		if _, err := b.tgService.Send(editMsg); err != nil {
			b.logger.Error().Err(err).Msg("Failed to send edit message in handleManagerDateType")
		}
	}

	if _, err := b.tgService.Send(tgbotapi.NewCallback(callback.ID, "")); err != nil {
		b.logger.Error().Err(err).Msg("Failed to send callback in handleManagerDateType")
	}
}

// handleManagerSingleDate обработка ввода одной даты
func (b *Bot) handleManagerSingleDate(ctx context.Context, update *tgbotapi.Update, dateStr string, state *models.UserState) {
	date, err := time.Parse("02.01.2006", dateStr)
	if err != nil {
		b.sendMessage(update.Message.Chat.ID, "Неверный формат даты. Используйте ДД.ММ.ГГГГ (например, 25.12.2024)")
		return
	}

	// Валидация даты через сервис
	if err := b.bookingService.ValidateBookingDate(date); err != nil {
		b.sendMessage(update.Message.Chat.ID, b.getErrorMessage(err))
		return
	}

	state.TempData["dates"] = []time.Time{date}
	b.setUserState(ctx, update.Message.From.ID, models.StateManagerWaitingComment, state.TempData)

	b.sendMessage(update.Message.Chat.ID, "💬 Введите комментарий к заявке "+
		"(например: 'Техническое обслуживание', 'Обучение персонала' или любой другой текст):")
}

// handleManagerStartDate обработка ввода начальной даты интервала
func (b *Bot) handleManagerStartDate(ctx context.Context, update *tgbotapi.Update, dateStr string, state *models.UserState) {
	startDate, err := time.Parse("02.01.2006", dateStr)
	if err != nil {
		b.sendMessage(update.Message.Chat.ID, "Неверный формат даты. Используйте ДД.ММ.ГГГГ (например, 25.12.2024)")
		return
	}

	// Валидация даты через сервис
	if err := b.bookingService.ValidateBookingDate(startDate); err != nil {
		b.sendMessage(update.Message.Chat.ID, b.getErrorMessage(err))
		return
	}

	state.TempData["start_date"] = startDate
	b.setUserState(ctx, update.Message.From.ID, models.StateManagerWaitingEndDate, state.TempData)

	b.sendMessage(update.Message.Chat.ID, "📅 Введите конечную дату интервала в формате ДД.ММ.ГГГГ:")
}

// handleManagerEndDate обработка ввода конечной даты интервала
func (b *Bot) handleManagerEndDate(ctx context.Context, update *tgbotapi.Update, dateStr string, state *models.UserState) {
	endDate, err := time.Parse("02.01.2006", dateStr)
	if err != nil {
		b.sendMessage(update.Message.Chat.ID, "Неверный формат даты. Используйте ДД.ММ.ГГГГ (например, 25.12.2024)")
		return
	}

	startDate := state.GetTime("start_date")

	// Проверяем, что конечная дата не раньше начальной
	if endDate.Before(startDate) {
		b.sendMessage(update.Message.Chat.ID, "Конечная дата не может быть раньше начальной.")
		return
	}

	// Валидация даты через сервис
	if err := b.bookingService.ValidateBookingDate(endDate); err != nil {
		b.sendMessage(update.Message.Chat.ID, b.getErrorMessage(err))
		return
	}

	// Ограничиваем интервал (например, максимум 31 день за раз)
	if endDate.Sub(startDate).Hours() > 24*31 {
		b.sendMessage(update.Message.Chat.ID, "Максимальный интервал бронирования - 31 день.")
		return
	}

	// Создаем список всех дат в интервале
	var dates []time.Time
	for d := startDate; !d.After(endDate); d = d.AddDate(0, 0, 1) {
		dates = append(dates, d)
	}

	state.TempData["dates"] = dates
	b.setUserState(ctx, update.Message.From.ID, models.StateManagerWaitingComment, state.TempData)

	b.sendMessage(update.Message.Chat.ID, fmt.Sprintf("💬 Введите комментарий к заявке (будет применен ко всем %d дням):", len(dates)))
}

// handleManagerComment обработка ввода комментария
func (b *Bot) handleManagerComment(ctx context.Context, update *tgbotapi.Update, comment string, state *models.UserState) {
	state.TempData["comment"] = b.sanitizeInput(comment)
	b.setUserState(ctx, update.Message.From.ID, models.StateManagerConfirmBooking, state.TempData)

	// Показываем подтверждение
	b.showManagerBookingConfirmation(ctx, update, state)
}

// showManagerBookingConfirmation показывает подтверждение заявки менеджером
func (b *Bot) showManagerBookingConfirmation(_ context.Context, update *tgbotapi.Update, state *models.UserState) {
	clientName := state.TempData["client_name"].(string)
	clientPhone := state.TempData["client_phone"].(string)
	itemID := state.GetInt64("item_id")
	selectedItem, _ := b.getItemByID(itemID)
	dates := state.GetDates("dates")
	comment := state.TempData["comment"].(string)
	dateType := state.TempData["date_type"].(string)

	var message strings.Builder
	message.WriteString("📋 *Подтверждение заявки:*\n\n")
	message.WriteString(fmt.Sprintf("👤 *Клиент:* %s\n", clientName))
	message.WriteString(fmt.Sprintf("📱 *Телефон:* %s\n", clientPhone))
	message.WriteString(fmt.Sprintf("🏢 *Аппарат:* %s\n", selectedItem.Name))

	if dateType == typeSingle {
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
	msg.ParseMode = models.ParseModeMarkdown

	if _, err := b.tgService.Send(msg); err != nil {
		b.logger.Error().Err(err).Msg("Failed to send message in showManagerBookingConfirmation")
	}
}

// createManagerBookings создает заявки менеджера
func (b *Bot) createManagerBookings(ctx context.Context, update *tgbotapi.Update, state *models.UserState) {
	clientName := state.TempData["client_name"].(string)
	clientPhone := state.TempData["client_phone"].(string)
	itemID := state.GetInt64("item_id")
	selectedItem, _ := b.getItemByID(itemID)
	dates := state.GetDates("dates")
	comment := state.TempData["comment"].(string)

	createdBookings := make([]*models.Booking, 0, len(dates))
	failedDates := make([]string, 0)

	// Создаем заявки на каждую дату
	for _, date := range dates {
		// Проверяем доступность
		available, err := b.bookingService.CheckAvailability(ctx, selectedItem.ID, date)
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

		start := time.Now()
		err = b.bookingService.CreateBooking(ctx, booking)
		if err != nil {
			b.logger.Error().Err(err).Interface("booking", booking).Msg("Error creating manager booking")
			failedDates = append(failedDates, fmt.Sprintf("%s (%s)", date.Format("02.01.2006"), b.getErrorMessage(err)))
		} else {
			createdBookings = append(createdBookings, booking)
			// Track metrics
			if b.metrics != nil {
				b.metrics.BookingsCreated.WithLabelValues(selectedItem.Name).Inc()
				b.metrics.BookingDuration.WithLabelValues(selectedItem.Name).Observe(time.Since(start).Seconds())
			}
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

// showManagerBookings показывает все заявки менеджеру с пагинацией
func (b *Bot) showManagerBookings(ctx context.Context, update *tgbotapi.Update) {
	if !b.isManager(update.Message.From.ID) {
		return
	}

	b.sendManagerBookingsPage(ctx, update.Message.Chat.ID, 0, 0)
}

// sendManagerBookingsPage отправляет страницу с заявками для менеджера
func (b *Bot) sendManagerBookingsPage(ctx context.Context, chatID int64, messageID, page int) {
	// Получаем все заявки за период: один месяц назад и два месяца вперед
	startDate := time.Now().AddDate(0, 0, -7) // 7 дней назад
	endDate := time.Now().AddDate(0, 2, 0)    // 2 месяца вперед

	bookings, err := b.bookingService.GetBookingsByDateRange(ctx, startDate, endDate)
	if err != nil {
		b.logger.Error().Err(err).Time("start_date", startDate).Time("end_date", endDate).Msg("Error getting bookings")
		b.sendMessage(chatID, "Ошибка при получении заявок")
		return
	}

	if len(bookings) == 0 {
		b.sendMessage(chatID, "Заявок не найдено")
		return
	}

	b.renderPaginatedBookings(&PaginationParams{
		Ctx:          ctx,
		ChatID:       chatID,
		MessageID:    messageID,
		Page:         page,
		Title:        "📊 *Все заявки на квартал вперед:*",
		ItemPrefix:   "show_booking:",
		PagePrefix:   "manager_bookings_page:",
		BackCallback: "back_to_main",
	}, bookings)
}

// showManagerBookingDetail показывает детали заявки менеджеру
func (b *Bot) showManagerBookingDetail(ctx context.Context, update *tgbotapi.Update, bookingID int64) {
	// ПРОВЕРКА НА NIL - чтобы избежать паники
	if update.Message == nil {
		b.logger.Error().Msg("Error: update.Message is nil in showManagerBookingDetail")
		return
	}

	booking, err := b.bookingService.GetBooking(ctx, bookingID)
	if err != nil {
		b.sendMessage(update.Message.Chat.ID, "Заявка не найдена")
		return
	}

	b.sendManagerBookingDetail(ctx, update.Message.Chat.ID, booking)
}

// startChangeItem начало изменения аппарата в заявке
func (b *Bot) startChangeItem(ctx context.Context, booking *models.Booking, managerChatID int64) {
	msg := tgbotapi.NewMessage(managerChatID,
		"Выберите новый аппарат для заявки #"+strconv.FormatInt(booking.ID, 10)+":")

	items, err := b.itemService.GetActiveItems(ctx)
	if err != nil {
		b.logger.Error().Err(err).Msg("Error getting active items")
		b.sendMessage(managerChatID, "Ошибка при получении списка аппаратов")
		return
	}

	keyboardRows := make([][]tgbotapi.InlineKeyboardButton, 0, len(items))
	for _, item := range items {
		row := tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(item.Name,
				fmt.Sprintf("change_to_%d_%d", booking.ID, item.ID)),
		)
		keyboardRows = append(keyboardRows, row)
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(keyboardRows...)
	msg.ReplyMarkup = &keyboard

	if _, err := b.tgService.Send(msg); err != nil {
		b.logger.Error().Err(err).Msg("Failed to send message in startChangeItem")
	}
}

// handleChangeItem обработка выбора нового аппарата С ПРОВЕРКОЙ ДОСТУПНОСТИ
func (b *Bot) handleChangeItem(ctx context.Context, update *tgbotapi.Update) {
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
	selectedItem, ok := b.getItemByID(itemID)

	if !ok {
		b.sendMessage(callback.Message.Chat.ID, "Аппарат не найден")
		return
	}

	// Получаем текущую заявку для версии
	booking, err := b.bookingService.GetBooking(ctx, bookingID)
	if err != nil {
		b.logger.Error().Err(err).Int64("booking_id", bookingID).Msg("Error getting booking")
		b.sendMessage(callback.Message.Chat.ID, "Ошибка при получении заявки")
		return
	}

	// Обновляем заявку через сервис
	err = b.bookingService.ChangeBookingItem(ctx, bookingID, booking.Version, selectedItem.ID, callback.From.ID)
	if err != nil {
		b.logger.Error().Err(err).Int64("booking_id", bookingID).Msg("Error changing booking item")
		b.sendMessage(callback.Message.Chat.ID, "Ошибка при изменении аппарата: "+b.getErrorMessage(err))
		return
	}

	b.logger.Info().
		Int64("booking_id", bookingID).
		Int64("manager_id", callback.From.ID).
		Int64("old_item_id", booking.ItemID).
		Int64("new_item_id", selectedItem.ID).
		Str("item_name", selectedItem.Name).
		Msg("Manager changed booking item")

	// Уведомляем пользователя
	userMsg := tgbotapi.NewMessage(booking.UserID,
		fmt.Sprintf("🔄 В вашей заявке #%d изменен аппарат на: %s", bookingID, selectedItem.Name))
	if _, errSend := b.tgService.Send(userMsg); errSend != nil {
		b.logger.Error().Err(errSend).Msg("Failed to send user notification in handleChangeItem")
	}

	b.sendMessage(callback.Message.Chat.ID, "✅ Аппарат успешно изменен")

	// ВМЕСТО ВЫЗОВА showManagerBookingDetail, который требует Message, используем sendManagerBookingDetail
	updatedBooking, err := b.bookingService.GetBooking(ctx, bookingID)
	if err != nil {
		b.logger.Error().Err(err).Int64("booking_id", bookingID).Msg("Error getting updated booking")
		return
	}

	// Отправляем обновленные детали заявки
	b.sendManagerBookingDetail(ctx, callback.Message.Chat.ID, updatedBooking)
}

// sendManagerBookingDetail отправляет детали заявки в указанный чат (без использования update)
func (b *Bot) sendManagerBookingDetail(_ context.Context, chatID int64, booking *models.Booking) {
	statusText := map[string]string{
		models.StatusPending:   "⏳ Ожидает подтверждения",
		models.StatusConfirmed: "✅ Подтверждена",
		models.StatusCanceled:  "❌ Отменена",
		models.StatusChanged:   "🔄 Изменена",
		models.StatusCompleted: "🏁 Завершена",
	}

	message := fmt.Sprintf(`📋 Заявка #%d

👤 Клиент: %s
📱 Телефон: %s
🏢 Позиция: %s
📅 Дата: %s
📊 Статус: %s
� Комментарий: %s
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

	msg := tgbotapi.NewMessage(chatID, message)

	// Создаем инлайн-клавиатуру для управления заявкой
	rows := make([][]tgbotapi.InlineKeyboardButton, 0, 4)

	if booking.Status == models.StatusPending || booking.Status == models.StatusChanged {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить", fmt.Sprintf("confirm_%d", booking.ID)),
			tgbotapi.NewInlineKeyboardButtonData("❌ Отклонить", fmt.Sprintf("reject_%d", booking.ID)),
		))
	}

	if booking.Status == models.StatusConfirmed {
		rows = append(rows,
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("🔄 Вернуть в работу", fmt.Sprintf("reopen_%d", booking.ID)),
				tgbotapi.NewInlineKeyboardButtonData("🏁 Завершить", fmt.Sprintf("complete_%d", booking.ID)),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✏️ Изменить аппарат", fmt.Sprintf("change_item_%d", booking.ID)),
				tgbotapi.NewInlineKeyboardButtonData("🔄 Предложить выбрать другую дату", fmt.Sprintf("reschedule_%d", booking.ID)),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("📞 Позвонить", fmt.Sprintf("call_booking:%d", booking.ID)),
			),
		)
	}

	if len(rows) > 0 {
		keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)
		msg.ReplyMarkup = &keyboard
	}

	if _, err := b.tgService.Send(msg); err != nil {
		b.logger.Error().Err(err).Msg("Failed to send message in sendManagerBookingDetail")
	}
}

// updateBookingStatus универсальный помощник для обновления статуса заявки
func (b *Bot) updateBookingStatus(ctx context.Context, booking *models.Booking, managerChatID int64, action string) {
	var err error
	var userMsgText, managerMsgText string
	var logMsg string

	switch action {
	case "reopen":
		logMsg = "Manager reopened booking"
		err = b.bookingService.ReopenBooking(ctx, booking.ID, booking.Version, managerChatID)
		userMsgText = fmt.Sprintf("🔄 Ваша заявка #%d возвращена в работу. Ожидайте подтверждения.", booking.ID)
		managerMsgText = "✅ Заявка возвращена в работу"
	case "complete":
		logMsg = "Manager completed booking"
		err = b.bookingService.CompleteBooking(ctx, booking.ID, booking.Version, managerChatID)
		userMsgText = fmt.Sprintf("🏁 Ваша заявка #%d завершена. Спасибо за использование наших услуг!", booking.ID)
		managerMsgText = "✅ Заявка завершена"
	case "confirm":
		logMsg = "Manager confirmed booking"
		err = b.bookingService.ConfirmBooking(ctx, booking.ID, booking.Version, managerChatID)
		userMsgText = fmt.Sprintf("✅ Ваша заявка на %s %s подтверждена!",
			booking.ItemName, booking.Date.Format("02.01.2006"))
		managerMsgText = "✅ Бронирование подтверждено"
	case "reject":
		logMsg = "Manager rejected booking"
		err = b.bookingService.RejectBooking(ctx, booking.ID, booking.Version, managerChatID)
		userMsgText = "❌ К сожалению, ваша заявка была отклонена менеджером."
		managerMsgText = "❌ Бронирование отменено"
	default:
		return
	}

	b.logger.Info().
		Int64("booking_id", booking.ID).
		Int64("manager_id", managerChatID).
		Int64("client_id", booking.UserID).
		Str("item_name", booking.ItemName).
		Msg(logMsg)

	if err != nil {
		if errors.Is(err, database.ErrConcurrentModification) {
			b.sendMessage(managerChatID, "Заявка уже изменена. Обновите данные и попробуйте снова.")
			return
		}
		b.logger.Error().Err(err).Int64("booking_id", booking.ID).Msg("Error updating booking status")
		return
	}

	// Уведомляем пользователя
	if _, err := b.tgService.Send(tgbotapi.NewMessage(booking.UserID, userMsgText)); err != nil {
		b.logger.Error().Err(err).Msg("Failed to send user notification")
	}

	// Уведомляем менеджера
	if _, err := b.tgService.Send(tgbotapi.NewMessage(managerChatID, managerMsgText)); err != nil {
		b.logger.Error().Err(err).Msg("Failed to send manager notification")
	}
}

// reopenBooking возврат заявки в работу
func (b *Bot) reopenBooking(ctx context.Context, booking *models.Booking, managerChatID int64) {
	b.updateBookingStatus(ctx, booking, managerChatID, "reopen")
}

// completeBooking завершение заявки
func (b *Bot) completeBooking(ctx context.Context, booking *models.Booking, managerChatID int64) {
	b.updateBookingStatus(ctx, booking, managerChatID, "complete")
}

// confirmBooking подтверждение бронирования менеджером
func (b *Bot) confirmBooking(ctx context.Context, booking *models.Booking, managerChatID int64) {
	b.updateBookingStatus(ctx, booking, managerChatID, "confirm")
}

// rejectBooking отклонение бронирования менеджером
func (b *Bot) rejectBooking(ctx context.Context, booking *models.Booking, managerChatID int64) {
	b.updateBookingStatus(ctx, booking, managerChatID, "reject")
}

// rescheduleBooking предложение выбрать другую дату
func (b *Bot) rescheduleBooking(ctx context.Context, booking *models.Booking, managerChatID int64) {
	b.logger.Info().
		Int64("booking_id", booking.ID).
		Int64("manager_id", managerChatID).
		Int64("client_id", booking.UserID).
		Str("item_name", booking.ItemName).
		Msg("Manager proposed reschedule")

	// Отправляем пользователю сообщение с предложением выбрать другую дату
	userMsg := tgbotapi.NewMessage(booking.UserID,
		fmt.Sprintf("🔄 Менеджер предложил выбрать другую дату для %s. Пожалуйста, создайте новую заявку.",
			booking.ItemName))

	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(btnCreateBooking),
		),
	)
	userMsg.ReplyMarkup = keyboard

	if _, err := b.tgService.Send(userMsg); err != nil {
		b.logger.Error().Err(err).Msg("Failed to send user msg in rescheduleBooking")
	}

	// Обновляем статус текущей заявки через сервис
	err := b.bookingService.RescheduleBooking(ctx, booking.ID, managerChatID)
	if err != nil {
		b.logger.Error().Err(err).Int64("booking_id", booking.ID).Msg("Error updating booking status")
	}

	managerMsg := tgbotapi.NewMessage(managerChatID, "🔄 Пользователю предложено выбрать другую дату")
	if _, err := b.tgService.Send(managerMsg); err != nil {
		b.logger.Error().Err(err).Msg("Failed to send manager msg in rescheduleBooking")
	}
}

// notifyManagers уведомление менеджеров о новой заявке
func (b *Bot) notifyManagers(booking *models.Booking) {
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
				tgbotapi.NewInlineKeyboardButtonData("🔄 Предложить другую дату", fmt.Sprintf("reschedule_%d", booking.ID)),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("📞 Позвонить", fmt.Sprintf("call_booking:%d", booking.ID)),
			),
		)
		msg.ReplyMarkup = &keyboard

		if _, err := b.tgService.Send(msg); err != nil {
			b.logger.Error().Err(err).Int64("manager_id", managerID).Msg("Failed to notify manager")
		}
	}
}

// handleCallButton обработка нажатия кнопки "Позвонить"
func (b *Bot) handleCallButton(ctx context.Context, update *tgbotapi.Update) {
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
		_, _ = b.tgService.Send(tgbotapi.NewCallback(callback.ID, "❌ Ошибка"))
		return
	}

	// Получаем заявку из базы данных
	booking, err := b.bookingService.GetBooking(ctx, bookingID)
	if err != nil {
		b.sendMessage(callback.Message.Chat.ID, "❌ Заявка не найдена")
		_, _ = b.tgService.Send(tgbotapi.NewCallback(callback.ID, "❌ Заявка не найдена"))
		return
	}

	if booking.Phone == "" {
		b.sendMessage(callback.Message.Chat.ID, "❌ Номер телефона не указан в заявке")
		_, _ = b.tgService.Send(tgbotapi.NewCallback(callback.ID, "❌ Номер не указан"))
		return
	}

	// Форматируем номер для отображения
	formattedPhone := b.formatPhoneForDisplay(booking.Phone)

	// Создаем информативное сообщение
	message := "📞 *Информация для связи*\n\n"
	message += fmt.Sprintf("👤 *Клиент:* %s\n", booking.UserName)
	message += fmt.Sprintf("📱 *Телефон:* `%s`\n", formattedPhone)
	message += fmt.Sprintf("🏢 *Аппарат:* %s\n", booking.ItemName)
	message += fmt.Sprintf("📅 *Дата:* %s\n", booking.Date.Format("02.01.2006"))

	if booking.Comment != "" {
		message += fmt.Sprintf("💬 *Комментарий:* %s\n", booking.Comment)
	}

	msg := tgbotapi.NewMessage(callback.Message.Chat.ID, message)
	msg.ParseMode = models.ParseModeMarkdown

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

	if _, err := b.tgService.Send(tgbotapi.NewCallback(callback.ID, "✅")); err != nil {
		b.logger.Error().Err(err).Msg("Failed to send callback in handleCallButton")
	}
	if _, err := b.tgService.Send(msg); err != nil {
		b.logger.Error().Err(err).Msg("Failed to send message in handleCallButton")
	}
}
