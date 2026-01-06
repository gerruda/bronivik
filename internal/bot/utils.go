package bot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"bronivik/internal/database"
	"bronivik/internal/models"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Вспомогательные методы для работы с состояниями пользователей

func (b *Bot) setUserState(ctx context.Context, userID int64, step string, tempData map[string]interface{}) {
	if tempData == nil {
		tempData = make(map[string]interface{})
	}

	err := b.stateService.SetUserState(ctx, userID, step, tempData)
	if err != nil {
		b.logger.Error().Err(err).Int64("user_id", userID).Str("step", step).Msg("Error setting user state")
	}
}

func (b *Bot) getUserState(ctx context.Context, userID int64) *models.UserState {
	state, err := b.stateService.GetUserState(ctx, userID)
	if err != nil {
		b.logger.Error().Err(err).Int64("user_id", userID).Msg("Error getting user state")
		return nil
	}
	return state
}

func (b *Bot) clearUserState(ctx context.Context, userID int64) {
	err := b.stateService.ClearUserState(ctx, userID)
	if err != nil {
		b.logger.Error().Err(err).Int64("user_id", userID).Msg("Error clearing user state")
	}
}

func (b *Bot) isBlacklisted(userID int64) bool {
	return b.userService.IsBlacklisted(userID)
}

func (b *Bot) isManager(userID int64) bool {
	return b.userService.IsManager(userID)
}

func (b *Bot) getItemByID(id int64) (models.Item, bool) {
	item, err := b.itemService.GetItemByID(context.Background(), id)
	if err != nil || item == nil {
		return models.Item{}, false
	}
	return *item, true
}

func (b *Bot) sendMessage(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	if _, err := b.tgService.Send(msg); err != nil {
		b.logger.Error().Err(err).Int64("chat_id", chatID).Msg("Failed to send message")
	}
}

// handleMainMenu - главное меню с контактами
func (b *Bot) handleMainMenu(ctx context.Context, update *tgbotapi.Update) {
	var userID int64
	var chatID int64

	// Определяем userID и chatID в зависимости от типа update
	switch {
	case update.Message != nil:
		userID = update.Message.From.ID
		chatID = update.Message.Chat.ID
	case update.CallbackQuery != nil:
		userID = update.CallbackQuery.From.ID
		chatID = update.CallbackQuery.Message.Chat.ID
	default:
		b.logger.Error().Msg("Error: cannot determine userID and chatID in handleMainMenu")
		return
	}

	b.updateUserActivity(userID)

	msg := tgbotapi.NewMessage(chatID,
		"Добро пожаловать! Выберите действие:")

	rows := make([][]tgbotapi.KeyboardButton, 0, 5)

	// Основные кнопки для всех пользователей
	if !b.isManager(userID) {
		rows = append(rows,
			tgbotapi.NewKeyboardButtonRow(
				tgbotapi.NewKeyboardButton(btnCreateBooking),
			),
			tgbotapi.NewKeyboardButtonRow(
				tgbotapi.NewKeyboardButton(btnViewSchedule),
				tgbotapi.NewKeyboardButton(btnAvailableItems),
			),
			tgbotapi.NewKeyboardButtonRow(
				tgbotapi.NewKeyboardButton(btnMyBookings),
				tgbotapi.NewKeyboardButton(btnManagerContacts),
			),
		)
	}

	// Кнопки только для менеджеров
	if b.isManager(userID) {
		rows = append(rows,
			tgbotapi.NewKeyboardButtonRow(
				tgbotapi.NewKeyboardButton(btnAllBookings),
			),
			tgbotapi.NewKeyboardButtonRow(
				tgbotapi.NewKeyboardButton(btnCreateBookingManager),
			),
			tgbotapi.NewKeyboardButtonRow(
				tgbotapi.NewKeyboardButton(btnSyncBookings),
				tgbotapi.NewKeyboardButton(btnSyncSchedule),
			),
		)
	}

	msg.ReplyMarkup = tgbotapi.NewReplyKeyboard(rows...)

	b.setUserState(ctx, userID, models.StateMainMenu, nil)
	if _, err := b.tgService.Send(msg); err != nil {
		b.logger.Error().Err(err).Int64("user_id", userID).Msg("Failed to send main menu")
	}
}

// showManagerContacts показывает контакты менеджеров
func (b *Bot) showManagerContacts(_ context.Context, update *tgbotapi.Update) {
	contacts := b.config.ManagersContacts
	var message strings.Builder
	message.WriteString("📞 Контакты менеджера:\n\n")
	for _, contact := range contacts {
		message.WriteString(fmt.Sprintf("🔹 %s\n", contact))
	}
	message.WriteString("\nПо любым интересующим Вас вопросам, дадим ответ.")

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, message.String())
	if _, err := b.tgService.Send(msg); err != nil {
		b.logger.Error().Err(err).Int64("chat_id", update.Message.Chat.ID).Msg("Failed to send manager contacts")
	}
}

// showUserBookings показывает заявки пользователя
func (b *Bot) showUserBookings(ctx context.Context, update *tgbotapi.Update) {
	bookings, err := b.userService.GetUserBookings(ctx, update.Message.From.ID)
	if err != nil {
		b.logger.Error().Err(err).Int64("user_id", update.Message.From.ID).Msg("Error getting user bookings")
		b.sendMessage(update.Message.Chat.ID, "Ошибка при получении заявок")
		return
	}

	var message strings.Builder
	message.WriteString("📊 Ваши заявки (за последние 2 недели и предстоящие):\n\n")

	for _, booking := range bookings {
		statusEmoji := statusPending
		switch booking.Status {
		case "confirmed":
			statusEmoji = statusSuccess
		case "canceled":
			statusEmoji = statusError
		case "changed":
			statusEmoji = "🔄"
		case "completed":
			statusEmoji = "🏁"
		}

		message.WriteString(fmt.Sprintf("%s Заявка #%d\n", statusEmoji, booking.ID))
		message.WriteString(fmt.Sprintf("   🏢 %s\n", booking.ItemName))
		message.WriteString(fmt.Sprintf("   📅 %s\n", booking.Date.Format("02.01.2006")))
		message.WriteString(fmt.Sprintf("   📊 Статус: %s\n\n", booking.Status))
	}

	if len(bookings) == 0 {
		message.WriteString("У вас пока нет заявок")
	}

	b.sendMessage(update.Message.Chat.ID, message.String())
}

// Добавляем метод для запроса имени
func (b *Bot) handleNameRequest(ctx context.Context, update *tgbotapi.Update) {
	b.debugState(ctx, update.Message.From.ID, "handleNameRequest START")

	msg := tgbotapi.NewMessage(update.Message.Chat.ID,
		"Пожалуйста, введите ваше ФИО для заявки:")

	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(btnManagerContacts),
			tgbotapi.NewKeyboardButton(btnCancel),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(btnBack),
		),
	)
	msg.ReplyMarkup = keyboard

	state := b.getUserState(ctx, update.Message.From.ID)

	b.setUserState(ctx, update.Message.From.ID, models.StateEnterName, state.TempData)

	b.debugState(ctx, update.Message.From.ID, "handleNameRequest END")
	if _, err := b.tgService.Send(msg); err != nil {
		b.logger.Error().Err(err).Int64("user_id", update.Message.From.ID).Msg("Failed to send name request")
	}
}

// Обновляем handlePhoneRequest - добавляем контакты
func (b *Bot) handlePhoneRequest(ctx context.Context, update *tgbotapi.Update) {
	msg := tgbotapi.NewMessage(update.Message.Chat.ID,
		"Пожалуйста, предоставьте ваш номер телефона для связи:\n"+
			"Вы можете предоставить разрешение на использование номера из контакта телеграмм\n"+
			"Либо введите номер телефона для связи")

	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButtonContact("📱 Отправить номер телефона из вашего контакта в телеграмм"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(btnManagerContacts),
			tgbotapi.NewKeyboardButton(btnCancel),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(btnBack),
		),
	)
	msg.ReplyMarkup = keyboard

	state := b.getUserState(ctx, update.Message.From.ID)

	b.setUserState(ctx, update.Message.From.ID, models.StatePhoneNumber, state.TempData)
	if _, err := b.tgService.Send(msg); err != nil {
		b.logger.Error().Err(err).Int64("user_id", update.Message.From.ID).Msg("Failed to send phone request")
	}
}

// finalizeBooking обновляем для использования имени
func (b *Bot) finalizeBooking(ctx context.Context, update *tgbotapi.Update) {
	state := b.getUserState(ctx, update.Message.From.ID)
	if state == nil {
		b.sendMessage(update.Message.Chat.ID, "Сессия устарела. Начните заново.")
		b.handleMainMenu(ctx, update)
		return
	}

	// Получаем данные из состояния
	itemID := state.GetInt64("item_id")
	date := state.GetTime("date")
	phone, _ := state.TempData["phone"].(string)
	userName, ok := state.TempData["user_name"].(string)
	if !ok {
		// Если имя не было введено, используем имя из Telegram
		userName = update.Message.From.FirstName + " " + update.Message.From.LastName
	}

	// Находим элемент по ID
	selectedItem, ok := b.getItemByID(itemID)

	if !ok {
		b.sendMessage(update.Message.Chat.ID, "Ошибка: выбранная позиция не найдена.")
		b.handleMainMenu(ctx, update)
		return
	}

	// Создаем бронирование
	booking := models.Booking{
		UserID:       update.Message.From.ID,
		UserName:     userName,
		UserNickname: update.Message.From.FirstName + " " + update.Message.From.LastName,
		Phone:        phone,
		ItemID:       selectedItem.ID,
		ItemName:     selectedItem.Name,
		Date:         date,
		Status:       "pending",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	start := time.Now()
	err := b.bookingService.CreateBooking(ctx, &booking)
	if err != nil {
		b.logger.Error().Err(err).Int64("user_id", update.Message.From.ID).Msg("Error creating booking")
		b.sendMessage(update.Message.Chat.ID, b.getErrorMessage(err))
		if errors.Is(err, database.ErrNotAvailable) || errors.Is(err, database.ErrPastDate) {
			b.handleMainMenu(ctx, update)
		}
		return
	}

	// Track metrics
	if b.metrics != nil {
		b.metrics.BookingsCreated.WithLabelValues(selectedItem.Name).Inc()
		b.metrics.BookingDuration.WithLabelValues(selectedItem.Name).Observe(time.Since(start).Seconds())
	}

	// Уведомляем менеджеров
	b.notifyManagers(&booking)

	msg := tgbotapi.NewMessage(update.Message.Chat.ID,
		fmt.Sprintf("⏳ Ваша заявка #%d на позицию %s успешно создана. \nОжидайте подтверждения.", booking.ID, booking.ItemName))

	// Очищаем состояние
	b.clearUserState(ctx, update.Message.From.ID)
	b.handleMainMenu(ctx, update)
	if _, err := b.tgService.Send(msg); err != nil {
		b.logger.Error().Err(err).Msg("Failed to send final booking msg")
	}
}

// handleContactReceived обработка полученного контакта
func (b *Bot) handleContactReceived(ctx context.Context, update *tgbotapi.Update) {
	state := b.getUserState(ctx, update.Message.From.ID)
	if state == nil {
		b.handleMainMenu(ctx, update)
		return
	}

	if state.CurrentStep == models.StatePhoneNumber {
		b.handlePhoneReceived(ctx, update, update.Message.Contact.PhoneNumber)
	}
}

// handleViewSchedule - меню просмотра расписания
func (b *Bot) handleViewSchedule(ctx context.Context, update *tgbotapi.Update) {
	b.updateUserActivity(update.Message.From.ID)

	// Сохраняем состояние выбора аппарата для расписания
	b.setUserState(ctx, update.Message.From.ID, "schedule_select_item", map[string]interface{}{
		"page": 0,
	})

	// Отправляем выбор аппарата
	b.sendScheduleItemsPage(ctx, update.Message.Chat.ID, 0, 0)
}

// sendScheduleItemsPage отправляет страницу с аппаратами для просмотра расписания
func (b *Bot) sendScheduleItemsPage(ctx context.Context, chatID int64, messageID, page int) {
	b.renderPaginatedItems(&PaginationParams{
		Ctx:          ctx,
		ChatID:       chatID,
		MessageID:    messageID,
		Page:         page,
		Title:        "🏢 *Выберите аппарат для просмотра расписания:*",
		ItemPrefix:   "schedule_select_item:",
		PagePrefix:   "schedule_items_page:",
		BackCallback: "back_to_main_from_schedule",
		ShowCapacity: false,
	})
}

func (b *Bot) handleSelectItem(ctx context.Context, update *tgbotapi.Update) {
	var chatID int64
	var userID int64
	var messageID int

	// Определяем источник вызова
	switch {
	case update.Message != nil:
		// Вызов из обычного сообщения
		chatID = update.Message.Chat.ID
		userID = update.Message.From.ID
	case update.CallbackQuery != nil:
		// Вызов из callback
		chatID = update.CallbackQuery.Message.Chat.ID
		userID = update.CallbackQuery.From.ID
		messageID = update.CallbackQuery.Message.MessageID

		// Отвечаем на callback (убираем "часики")
		callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "")
		if _, err := b.tgService.Request(callbackConfig); err != nil {
			b.logger.Error().Err(err).Msg("Failed to answer callback query")
		}
	default:
		b.logger.Error().Msg("Error: cannot determine chatID and userID in handleSelectItem")
		return
	}

	// Обновляем активность пользователя
	b.updateUserActivity(userID)

	// Сохраняем состояние
	b.setUserState(ctx, userID, models.StateSelectItem, map[string]interface{}{
		"page": 0,
	})

	// Отправляем первую страницу
	b.sendItemsPage(ctx, chatID, messageID, 0)
}

// sendItemsPage отправляет страницу с аппаратами
func (b *Bot) sendItemsPage(ctx context.Context, chatID int64, messageID, page int) {
	b.renderPaginatedItems(&PaginationParams{
		Ctx:          ctx,
		ChatID:       chatID,
		MessageID:    messageID,
		Page:         page,
		Title:        "🏢 *Доступные аппараты*",
		ItemPrefix:   "select_item:",
		PagePrefix:   "items_page:",
		BackCallback: "back_to_main",
		ShowCapacity: false,
	})
}

// showAvailableItems показывает доступные позиции
func (b *Bot) showAvailableItems(ctx context.Context, update *tgbotapi.Update) {
	items, err := b.itemService.GetActiveItems(ctx)
	if err != nil {
		b.logger.Error().Err(err).Msg("Error getting active items")
		b.sendMessage(update.Message.Chat.ID, "Ошибка при получении списка аппаратов")
		return
	}
	var message strings.Builder
	message.WriteString("🏢 Доступные позиции:\n\n")

	for _, item := range items {
		message.WriteString(fmt.Sprintf("🔹 %s\n", item.Name))
		message.WriteString(fmt.Sprintf("   %s\n", item.Description))
		message.WriteString("\n")
	}

	keyboard := make([][]tgbotapi.InlineKeyboardButton, 0, 1)

	keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData(btnCreateBooking, "start_the_order"),
	})

	markup := tgbotapi.NewInlineKeyboardMarkup(keyboard...)

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, message.String())
	msg.ReplyMarkup = &markup

	if _, err := b.tgService.Send(msg); err != nil {
		b.logger.Error().Err(err).Int64("chat_id", update.Message.Chat.ID).Msg("Failed to send available items")
	}
}

// showMonthScheduleForItem показывает расписание на 30 дней для выбранного аппарата
func (b *Bot) showMonthScheduleForItem(ctx context.Context, update *tgbotapi.Update) {
	state := b.getUserState(ctx, update.Message.From.ID)
	if state == nil || state.TempData["item_id"] == nil {
		b.sendMessage(update.Message.Chat.ID, "Ошибка: аппарат не выбран")
		return
	}

	itemID := state.GetInt64("item_id")
	selectedItem, ok := b.getItemByID(itemID)
	if !ok {
		b.sendMessage(update.Message.Chat.ID, "Ошибка: аппарат не найден")
		return
	}
	startDate := time.Now()

	availability, err := b.bookingService.GetAvailability(ctx, selectedItem.ID, startDate, 30)
	if err != nil {
		b.logger.Error().Err(err).Int64("item_id", selectedItem.ID).Msg("Error getting availability")
		b.sendMessage(update.Message.Chat.ID, "Ошибка при получении расписания")
		return
	}

	var message strings.Builder
	message.WriteString(fmt.Sprintf("📅 *Расписание %s*\n", selectedItem.Name))
	message.WriteString("На ближайшие 30 дней:\n\n")

	message.WriteString("```\n")
	message.WriteString("Дата     Статус\n")
	message.WriteString("───────  ──────────\n")

	for _, avail := range availability {
		status := "✅ Свободно"
		if avail.Available == 0 {
			status = "❌ Занято  "
		}

		message.WriteString(fmt.Sprintf("%s   %s\n",
			avail.Date.Format("02.01"), status))
	}
	message.WriteString("```")

	keyboard := make([][]tgbotapi.InlineKeyboardButton, 0, 1)

	keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData(btnCreateForItem, "start_the_order_item"),
	})

	markup := tgbotapi.NewInlineKeyboardMarkup(keyboard...)

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, message.String())
	msg.ReplyMarkup = &markup
	msg.ParseMode = models.ParseModeMarkdown
	if _, err := b.tgService.Send(msg); err != nil {
		b.logger.Error().Err(err).Int64("chat_id", update.Message.Chat.ID).Msg("Failed to send month schedule")
	}
}

// handleSpecificDateInput обновляем для работы с выбранным аппаратом
func (b *Bot) handleSpecificDateInput(ctx context.Context, update *tgbotapi.Update, dateStr string) {
	state := b.getUserState(ctx, update.Message.From.ID)
	if state == nil || state.TempData["item_id"] == nil {
		b.sendMessage(update.Message.Chat.ID, "Ошибка: аппарат не выбран")
		return
	}

	itemID := state.GetInt64("item_id")
	selectedItem, ok := b.getItemByID(itemID)
	if !ok {
		b.sendMessage(update.Message.Chat.ID, "Ошибка: аппарат не найден")
		return
	}

	date, err := time.Parse("02.01.2006", dateStr)
	if err != nil {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID,
			"Неверный формат даты. Используйте ДД.ММ.ГГГГ (например, 25.12.2024)")
		if _, errSend := b.tgService.Send(msg); errSend != nil {
			b.logger.Error().Err(errSend).Msg("Failed to send invalid date format msg in handleSpecificDateInput")
		}
		return
	}

	available, err := b.bookingService.CheckAvailability(ctx, selectedItem.ID, date)
	if err != nil {
		b.logger.Error().Err(err).Int64("item_id", selectedItem.ID).Time("date", date).Msg("Error checking availability")
		b.sendMessage(update.Message.Chat.ID, "Ошибка при проверке доступности")
		return
	}

	status := "✅ Доступно"
	if !available {
		status = "❌ Недоступно"
	}

	booked, _ := b.bookingService.GetBookedCount(ctx, selectedItem.ID, date)
	message := fmt.Sprintf("📅 Доступность *%s* на %s:\n\n%s\n\nЗабронировано: %d/%d",
		selectedItem.Name,
		date.Format("02.01.2006"),
		status,
		booked,
		selectedItem.TotalQuantity)

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, message)
	msg.ParseMode = models.ParseModeMarkdown
	if _, err := b.tgService.Send(msg); err != nil {
		b.logger.Error().Err(err).Msg("Failed to send specific date info in handleSpecificDateInput")
	}
}

// requestSpecificDate запрашивает у пользователя конкретную дату
func (b *Bot) requestSpecificDate(ctx context.Context, update *tgbotapi.Update) {
	msg := tgbotapi.NewMessage(update.Message.Chat.ID,
		"Введите дату в формате ДД.ММ.ГГГГ (например, 25.12.2025):")

	b.setUserState(ctx, update.Message.From.ID, models.StateWaitingSpecificDate, nil)
	if _, err := b.tgService.Send(msg); err != nil {
		b.logger.Error().Err(err).Msg("Failed to send requestSpecificDate message")
	}
}

// handleCustomInput ...
func (b *Bot) handleCustomInput(ctx context.Context, update *tgbotapi.Update, state *models.UserState) {
	if state == nil {
		b.sendMessage(update.Message.Chat.ID, "Неизвестная команда. Используйте меню.")
		b.handleMainMenu(ctx, update)
		return
	}

	text := update.Message.Text
	userID := update.Message.From.ID

	// Обработка общих кнопок "Назад" и "Отмена"
	if text == btnCancel {
		b.clearUserState(ctx, userID)
		b.sendMessage(update.Message.Chat.ID, "❌ Действие отменено")
		b.handleMainMenu(ctx, update)
		return
	}

	if text == btnBack {
		switch state.CurrentStep {
		case models.StateEnterName:
			b.handleDateSelection(ctx, update, state.GetInt64("item_id"))
			return
		case models.StatePhoneNumber:
			b.handleNameRequest(ctx, update)
			return
		case models.StateWaitingDate:
			b.handleSelectItem(ctx, update)
			return
		}
	}

	b.sendMessage(update.Message.Chat.ID, "Неизвестная команда. Используйте меню.")
	b.handleMainMenu(ctx, update)
}

// sanitizeInput удаляет потенциально опасные символы из ввода
func (b *Bot) sanitizeInput(input string) string {
	// Ограничиваем длину
	if len(input) > 500 {
		input = input[:500]
	}
	// Удаляем управляющие символы и HTML-теги (простейшая очистка)
	replacer := strings.NewReplacer(
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&#39;",
		"\n", " ",
		"\r", " ",
		"\t", " ",
	)
	return strings.TrimSpace(replacer.Replace(input))
}

// handleDateInput обрабатывает ввод даты для бронирования
func (b *Bot) handleDateInput(ctx context.Context, update *tgbotapi.Update, dateStr string, state *models.UserState) {
	b.debugState(ctx, update.Message.From.ID, "handleDateInput START")

	date, err := time.Parse("02.01.2006", dateStr)
	if err != nil {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID,
			"Неверный формат даты. Используйте ДД.ММ.ГГГГ (например, 25.12.2024)")
		if _, errSend := b.tgService.Send(msg); errSend != nil {
			b.logger.Error().Err(errSend).Msg("Failed to send invalid date format msg in handleDateInput")
		}
		return
	}

	// Валидация даты через сервис
	if errVal := b.bookingService.ValidateBookingDate(date); errVal != nil {
		b.sendMessage(update.Message.Chat.ID, b.getErrorMessage(errVal))
		return
	}

	itemID := state.GetInt64("item_id")
	item, ok := b.getItemByID(itemID)

	if !ok {
		b.sendMessage(update.Message.Chat.ID, "Ошибка: не найден выбранный элемент. Начните заново.")
		b.handleMainMenu(ctx, update)
		return
	}

	// Проверяем доступность
	available, err := b.bookingService.CheckAvailability(ctx, item.ID, date)
	if err != nil {
		b.logger.Error().Err(err).Int64("item_id", item.ID).Time("date", date).Msg("Error checking availability")
		msg := tgbotapi.NewMessage(update.Message.Chat.ID,
			"Произошла ошибка при проверке доступности. Попробуйте позже.")
		if _, errSend := b.tgService.Send(msg); errSend != nil {
			b.logger.Error().Err(errSend).Msg("Failed to send error msg in handleDateInput")
		}
		return
	}

	if !available {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID,
			"К сожалению, на выбранную дату позиция недоступна. Выберите другую дату.")
		if _, errSend := b.tgService.Send(msg); errSend != nil {
			b.logger.Error().Err(errSend).Msg("Failed to send not available msg in handleDateInput")
		}
		return
	}

	// Сохраняем данные в состоянии перед переходом
	state.TempData["item_id"] = item.ID
	state.TempData["date"] = date
	b.setUserState(ctx, update.Message.From.ID, "waiting_date", state.TempData)

	b.debugState(ctx, update.Message.From.ID, "handleDateInput END")

	// Переходим к запросу персональных данных
	b.handleNameRequest(ctx, update)
}

// restoreStateOrRestart восстанавливает состояние или начинает заново
func (b *Bot) restoreStateOrRestart(ctx context.Context, update *tgbotapi.Update, requiredFields ...string) bool {
	state := b.getUserState(ctx, update.Message.From.ID)
	if state == nil {
		b.sendMessage(update.Message.Chat.ID, "Сессия устарела. Начните заново.")
		b.handleMainMenu(ctx, update)
		return false
	}

	for _, field := range requiredFields {
		if _, exists := state.TempData[field]; !exists {
			b.sendMessage(update.Message.Chat.ID,
				fmt.Sprintf("Ошибка: отсутствуют данные (%s). Начните заново.", field))
			b.handleMainMenu(ctx, update)
			return false
		}
	}

	return true
}

// Добавьте этот метод в utils.go для отладки
func (b *Bot) debugState(ctx context.Context, userID int64, message string) {
	state := b.getUserState(ctx, userID)
	if state != nil {
		b.logger.Debug().
			Int64("user_id", userID).
			Str("step", state.CurrentStep).
			Interface("temp_data", state.TempData).
			Msg(message)
	} else {
		b.logger.Debug().Int64("user_id", userID).Msg(message + " (state is nil)")
	}
}

// handlePhoneReceived обработка полученного номера телефона
func (b *Bot) handlePhoneReceived(ctx context.Context, update *tgbotapi.Update, phone string) {
	b.debugState(ctx, update.Message.From.ID, "handlePhoneReceived START")

	// Проверяем и восстанавливаем состояние
	if !b.restoreStateOrRestart(ctx, update, "item_id", "date") {
		return
	}

	state := b.getUserState(ctx, update.Message.From.ID)

	// Проверяем и нормализуем номер телефона
	normalizedPhone := b.normalizePhone(phone)
	if normalizedPhone == "" {
		b.sendMessage(update.Message.Chat.ID, "Неверный формат номера телефона. Пожалуйста, введите номер в формате +7XXXXXXXXXX или 8XXXXXXXXXX")
		return
	}

	// Получаем данные из состояния
	itemID := state.GetInt64("item_id")
	date := state.GetTime("date")

	// Находим выбранный элемент по ID
	selectedItem, ok := b.getItemByID(itemID)

	if !ok {
		b.sendMessage(update.Message.Chat.ID, "Ошибка: выбранная позиция не найдена. Начните заново.")
		b.handleMainMenu(ctx, update)
		return
	}

	state.TempData["phone"] = normalizedPhone
	state.TempData["item_id"] = selectedItem.ID // Сохраняем ID элемента для подтверждения
	b.setUserState(ctx, update.Message.From.ID, models.StateConfirmation, state.TempData)

	b.debugState(ctx, update.Message.From.ID, "handlePhoneReceived END")

	// Сохраняем телефон пользователя
	b.updateUserPhone(update.Message.From.ID, normalizedPhone)

	// Проверяем доступность еще раз
	available, err := b.bookingService.CheckAvailability(ctx, selectedItem.ID, date)
	if err != nil || !available {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID,
			"К сожалению, выбранная позиция больше не доступна на эту дату. Пожалуйста, начните заново.")
		if _, errSend := b.tgService.Send(msg); errSend != nil {
			b.logger.Error().Err(errSend).Msg("Failed to send not available msg in handlePhoneReceived")
		}
		b.handleMainMenu(ctx, update)
		return
	}

	name, ok := state.TempData["user_name"].(string)
	if !ok {
		name = ""
	}

	msg := tgbotapi.NewMessage(update.Message.Chat.ID,
		fmt.Sprintf(`📋 Подтверждение заявки:

🏢 Позиция: %s
📅 Дата: %s
👤 Имя: %s
📱 Телефон: %s`,
			selectedItem.Name,
			date.Format("02.01.2006"),
			name,
			normalizedPhone))

	if _, err := b.tgService.Send(msg); err != nil {
		b.logger.Error().Err(err).Msg("Failed to send confirmation msg in handlePhoneReceived")
	}
	b.finalizeBooking(ctx, update)
}

// normalizePhone нормализует номер телефона
func (b *Bot) normalizePhone(phone string) string {
	// Удаляем все нецифровые символы
	cleaned := ""
	for _, char := range phone {
		if char >= '0' && char <= '9' {
			cleaned += string(char)
		}
	}

	// Обрабатываем разные форматы номеров
	if len(cleaned) == 11 {
		if cleaned[0] == '8' {
			return "7" + cleaned[1:] // 8XXXXXXXXXX -> 7XXXXXXXXXX
		} else if cleaned[0] == '7' {
			return cleaned // 7XXXXXXXXXX
		}
	} else if len(cleaned) == 10 {
		return "7" + cleaned // XXXXXXXXXX -> 7XXXXXXXXXX
	}

	return "" // Неверный формат
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
