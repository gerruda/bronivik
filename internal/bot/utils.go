package bot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"bronivik/internal/database"
	"bronivik/internal/events"
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
	if state == nil {
		return nil
	}

	return &models.UserState{
		UserID:      state.UserID,
		CurrentStep: state.Step,
		TempData:    state.Data,
	}
}

func (b *Bot) clearUserState(ctx context.Context, userID int64) {
	err := b.stateService.ClearUserState(ctx, userID)
	if err != nil {
		b.logger.Error().Err(err).Int64("user_id", userID).Msg("Error clearing user state")
	}
}

func (b *Bot) isBlacklisted(userID int64) bool {
	for _, blacklistedID := range b.config.Blacklist {
		if userID == blacklistedID {
			return true
		}
	}
	return false
}

func (b *Bot) isManager(userID int64) bool {
	for _, managerID := range b.config.Managers {
		if userID == managerID {
			return true
		}
	}
	return false
}

func (b *Bot) sendMessage(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	b.bot.Send(msg)
}

func (b *Bot) publishBookingEvent(ctx context.Context, eventType string, booking models.Booking, changedBy string, changedByID int64) {
	if b.eventBus == nil {
		return
	}

	payload := events.BookingEventPayload{
		BookingID:   booking.ID,
		UserID:      booking.UserID,
		UserName:    booking.UserName,
		ItemID:      booking.ItemID,
		ItemName:    booking.ItemName,
		Status:      booking.Status,
		Date:        booking.Date,
		Comment:     booking.Comment,
		ChangedBy:   changedBy,
		ChangedByID: changedByID,
	}

	if err := b.eventBus.PublishJSON(eventType, payload); err != nil {
		b.logger.Error().Err(err).Str("event_type", eventType).Int64("booking_id", booking.ID).Msg("publish event error")
	}
}

// handleMainMenu - главное меню с контактами
func (b *Bot) handleMainMenu(ctx context.Context, update tgbotapi.Update) {
	var userID int64
	var chatID int64

	// Определяем userID и chatID в зависимости от типа update
	if update.Message != nil {
		userID = update.Message.From.ID
		chatID = update.Message.Chat.ID
	} else if update.CallbackQuery != nil {
		userID = update.CallbackQuery.From.ID
		chatID = update.CallbackQuery.Message.Chat.ID
	} else {
		b.logger.Error().Msg("Error: cannot determine userID and chatID in handleMainMenu")
		return
	}

	b.updateUserActivity(userID)

	msg := tgbotapi.NewMessage(chatID,
		"Добро пожаловать! Выберите действие:")

	var rows [][]tgbotapi.KeyboardButton

	// Основные кнопки для всех пользователей
	if !b.isManager(userID) {
		rows = append(rows, tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📋 СОЗДАТЬ ЗАЯВКУ"),
		))
		rows = append(rows, tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📅 Посмотреть расписание"),
			tgbotapi.NewKeyboardButton("💼 Ассортимент"),
		))

		rows = append(rows, tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📊 Мои заявки"),
			tgbotapi.NewKeyboardButton("📞 Контакты менеджеров"),
		))
	}

	// Кнопки только для менеджеров
	if b.isManager(userID) {
		rows = append(rows, tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("👨‍💼 Все заявки"),
		))
		rows = append(rows, tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("➕ Создать заявку (Менеджер)"),
		))
		rows = append(rows, tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🔄 Синхронизировать список заявок (Google Sheets)"),
			tgbotapi.NewKeyboardButton("📅 Синхронизировать расписание (Google Sheets)"),
		))
	}

	msg.ReplyMarkup = tgbotapi.NewReplyKeyboard(rows...)

	b.setUserState(ctx, userID, StateMainMenu, nil)
	b.bot.Send(msg)
}

// showManagerContacts показывает контакты менеджеров
func (b *Bot) showManagerContacts(ctx context.Context, update tgbotapi.Update) {
	contacts := b.config.ManagersContacts
	var message strings.Builder
	message.WriteString("📞 Контакты менеджера:\n\n")
	for _, contact := range contacts {
		message.WriteString(fmt.Sprintf("🔹 %s\n", contact))
	}
	message.WriteString("\nПо любым интересующим Вас вопросам, дадим ответ.")

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, message.String())
	b.bot.Send(msg)
}

// showUserBookings показывает заявки пользователя
func (b *Bot) showUserBookings(ctx context.Context, update tgbotapi.Update) {
	bookings, err := b.db.GetUserBookings(ctx, update.Message.From.ID)
	if err != nil {
		b.logger.Error().Err(err).Int64("user_id", update.Message.From.ID).Msg("Error getting user bookings")
		b.sendMessage(update.Message.Chat.ID, "Ошибка при получении заявок")
		return
	}

	var message strings.Builder
	message.WriteString("📊 Ваши заявки (за последние 2 недели и предстоящие):\n\n")

	for _, booking := range bookings {
		statusEmoji := "⏳"
		switch booking.Status {
		case "confirmed":
			statusEmoji = "✅"
		case "cancelled":
			statusEmoji = "❌"
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

// Обновляем handlePersonalData - добавляем запрос имени
func (b *Bot) handlePersonalData(ctx context.Context, update tgbotapi.Update, itemID int64, date time.Time) {
	state := b.getUserState(ctx, update.Message.From.ID)
	if state == nil {
		state = &models.UserState{
			UserID:   update.Message.From.ID,
			TempData: make(map[string]interface{}),
		}
	}

	state.TempData["item_id"] = itemID
	state.TempData["date"] = date
	b.setUserState(ctx, update.Message.From.ID, StatePersonalData, state.TempData)

	msg := tgbotapi.NewMessage(update.Message.Chat.ID,
		`Для оформления заявки необходимо ваше согласие на обработку персональных данных.
        
Мы обязуемся использовать ваши данные исключительно для обработки заявки и связи с вами.`)

	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("✅ Даю согласие"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📞 Контакты менеджеров"),
			tgbotapi.NewKeyboardButton("❌ Отмена"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("⬅️ Назад"),
		),
	)
	msg.ReplyMarkup = keyboard

	b.bot.Send(msg)
}

// Добавляем метод для запроса имени
func (b *Bot) handleNameRequest(ctx context.Context, update tgbotapi.Update) {
	b.debugState(ctx, update.Message.From.ID, "handleNameRequest START")

	msg := tgbotapi.NewMessage(update.Message.Chat.ID,
		"Пожалуйста, введите ваше ФИО для заявки:")

	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📞 Контакты менеджеров"),
			tgbotapi.NewKeyboardButton("❌ Отмена"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("⬅️ Назад"),
		),
	)
	msg.ReplyMarkup = keyboard

	state := b.getUserState(ctx, update.Message.From.ID)

	b.setUserState(ctx, update.Message.From.ID, StateEnterName, state.TempData)

	b.debugState(ctx, update.Message.From.ID, "handleNameRequest END")
	b.bot.Send(msg)
}

// Обновляем handlePhoneRequest - добавляем контакты
func (b *Bot) handlePhoneRequest(ctx context.Context, update tgbotapi.Update) {
	msg := tgbotapi.NewMessage(update.Message.Chat.ID,
		"Пожалуйста, предоставьте ваш номер телефона для связи:\n"+
			"Вы можете предоставить разрешение на использование номера из контакта телеграмм\n"+
			"Либо введите номер телефона для связи")

	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButtonContact("📱 Отправить номер телефона из вашего контакта в телеграмм"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📞 Контакты менеджеров"),
			tgbotapi.NewKeyboardButton("❌ Отмена"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("⬅️ Назад"),
		),
	)
	msg.ReplyMarkup = keyboard

	state := b.getUserState(ctx, update.Message.From.ID)

	b.setUserState(ctx, update.Message.From.ID, StatePhoneNumber, state.TempData)
	b.bot.Send(msg)
}

func (b *Bot) getInt64FromTempData(data map[string]interface{}, key string) int64 {
	val, ok := data[key]
	if !ok {
		return 0
	}
	switch v := val.(type) {
	case int64:
		return v
	case float64:
		return int64(v)
	case int:
		return int64(v)
	default:
		return 0
	}
}

func (b *Bot) getTimeFromTempData(data map[string]interface{}, key string) time.Time {
	val, ok := data[key]
	if !ok {
		return time.Time{}
	}
	switch v := val.(type) {
	case time.Time:
		return v
	case string:
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			// Try other formats if needed
			t, err = time.Parse("2006-01-02T15:04:05Z07:00", v)
			if err != nil {
				return time.Time{}
			}
		}
		return t
	default:
		return time.Time{}
	}
}

func (b *Bot) getDatesFromTempData(data map[string]interface{}, key string) []time.Time {
	val, ok := data[key]
	if !ok {
		return nil
	}
	switch v := val.(type) {
	case []time.Time:
		return v
	case []interface{}:
		var dates []time.Time
		for _, item := range v {
			if s, ok := item.(string); ok {
				t, err := time.Parse(time.RFC3339, s)
				if err != nil {
					t, err = time.Parse("2006-01-02T15:04:05Z07:00", s)
				}
				if err == nil {
					dates = append(dates, t)
				}
			} else if t, ok := item.(time.Time); ok {
				dates = append(dates, t)
			}
		}
		return dates
	default:
		return nil
	}
}

func (b *Bot) getStringFromTempData(data map[string]interface{}, key string) string {
	val, ok := data[key]
	if !ok {
		return ""
	}
	if s, ok := val.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", val)
}

// Обновляем finalizeBooking для использования имени
func (b *Bot) finalizeBooking(ctx context.Context, update tgbotapi.Update) {
	state := b.getUserState(ctx, update.Message.From.ID)
	if state == nil {
		b.sendMessage(update.Message.Chat.ID, "Сессия устарела. Начните заново.")
		b.handleMainMenu(ctx, update)
		return
	}

	// Получаем данные из состояния
	itemID := b.getInt64FromTempData(state.TempData, "item_id")
	date := b.getTimeFromTempData(state.TempData, "date")
	phone, _ := state.TempData["phone"].(string)
	userName, ok := state.TempData["user_name"].(string)
	if !ok {
		// Если имя не было введено, используем имя из Telegram
		userName = update.Message.From.FirstName + " " + update.Message.From.LastName
	}

	// Находим элемент по ID
	var selectedItem models.Item
	for _, item := range b.items {
		if item.ID == itemID {
			selectedItem = item
			break
		}
	}

	if selectedItem.ID == 0 {
		b.sendMessage(update.Message.Chat.ID, "Ошибка: выбранная позиция не найдена.")
		b.handleMainMenu(ctx, update)
		return
	}

	// Финальная проверка доступности
	available, err := b.db.CheckAvailability(ctx, selectedItem.ID, date)
	if err != nil || !available {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID,
			"К сожалению, выбранная позиция больше не доступна. Пожалуйста, выберите другую дату.")
		b.bot.Send(msg)
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

	err = b.db.CreateBookingWithLock(ctx, &booking)
	if err != nil {
		if errors.Is(err, database.ErrNotAvailable) {
			b.sendMessage(update.Message.Chat.ID, "К сожалению, позиция стала недоступна. Попробуйте выбрать другую дату.")
			b.handleMainMenu(ctx, update)
			return
		}
		b.logger.Error().Err(err).Int64("user_id", update.Message.From.ID).Msg("Error creating booking")
		b.sendMessage(update.Message.Chat.ID, "Произошла ошибка при создании заявки. Попробуйте позже.")
		return
	}

	b.publishBookingEvent(ctx, events.EventBookingCreated, booking, "user", update.Message.From.ID)

	// Уведомляем менеджеров
	b.notifyManagers(booking)

	msg := tgbotapi.NewMessage(update.Message.Chat.ID,
		fmt.Sprintf("⏳ Ваша заявка #%d на позицию %s успешно создана. \nОжидайте подтверждения.", booking.ID, booking.ItemName))

	// Очищаем состояние
	b.clearUserState(ctx, update.Message.From.ID)
	b.handleMainMenu(ctx, update)
	b.bot.Send(msg)
}

// handleContactReceived обработка полученного контакта
func (b *Bot) handleContactReceived(ctx context.Context, update tgbotapi.Update) {
	state := b.getUserState(ctx, update.Message.From.ID)
	if state == nil {
		b.handleMainMenu(ctx, update)
		return
	}

	if state.CurrentStep == StatePhoneNumber {
		b.handlePhoneReceived(ctx, update, update.Message.Contact.PhoneNumber)
	}
}

// handleViewSchedule - меню просмотра расписания
func (b *Bot) handleViewSchedule(ctx context.Context, update tgbotapi.Update) {
	b.updateUserActivity(update.Message.From.ID)

	// Сохраняем состояние выбора аппарата для расписания
	b.setUserState(ctx, update.Message.From.ID, "schedule_select_item", map[string]interface{}{
		"page": 0,
	})

	// Отправляем выбор аппарата
	b.sendScheduleItemsPage(ctx, update.Message.Chat.ID, update.Message.From.ID, 0)
}

// sendScheduleItemsPage отправляет страницу с аппаратами для просмотра расписания
func (b *Bot) sendScheduleItemsPage(ctx context.Context, chatID, userID int64, page int) {
	itemsPerPage := 8
	startIdx := page * itemsPerPage
	endIdx := startIdx + itemsPerPage
	if endIdx > len(b.items) {
		endIdx = len(b.items)
	}

	var message strings.Builder
	message.WriteString("🏢 *Выберите аппарат для просмотра расписания:*\n\n")
	message.WriteString(fmt.Sprintf("Страница %d из %d\n\n", page+1, (len(b.items)+itemsPerPage-1)/itemsPerPage))

	currentItems := b.items[startIdx:endIdx]
	for i, item := range currentItems {
		message.WriteString(fmt.Sprintf("%d. *%s*\n", startIdx+i+1, item.Name))
		message.WriteString(fmt.Sprintf("   📝 %s\n", item.Description))
	}

	var keyboard [][]tgbotapi.InlineKeyboardButton

	// Кнопки выбора аппаратов для расписания
	for i, item := range currentItems {
		btn := tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("%d. %s", startIdx+i+1, item.Name),
			fmt.Sprintf("schedule_select_item:%d", item.ID),
		)
		keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{btn})
	}

	// Кнопки навигации
	var navButtons []tgbotapi.InlineKeyboardButton

	if page > 0 {
		navButtons = append(navButtons, tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", fmt.Sprintf("schedule_items_page:%d", page-1)))
	}

	if endIdx < len(b.items) {
		navButtons = append(navButtons, tgbotapi.NewInlineKeyboardButtonData("Вперед ➡️", fmt.Sprintf("schedule_items_page:%d", page+1)))
	}

	if len(navButtons) > 0 {
		keyboard = append(keyboard, navButtons)
	}

	// Кнопка возврата
	keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад в меню", "back_to_main_from_schedule"),
	})

	markup := tgbotapi.NewInlineKeyboardMarkup(keyboard...)

	msg := tgbotapi.NewMessage(chatID, message.String())
	msg.ReplyMarkup = &markup
	msg.ParseMode = "Markdown"

	b.bot.Send(msg)
}

func (b *Bot) handleSelectItem(ctx context.Context, update tgbotapi.Update) {
	var chatID int64
	var userID int64

	// Определяем источник вызова
	if update.Message != nil {
		// Вызов из обычного сообщения
		chatID = update.Message.Chat.ID
		userID = update.Message.From.ID
	} else if update.CallbackQuery != nil {
		// Вызов из callback
		chatID = update.CallbackQuery.Message.Chat.ID
		userID = update.CallbackQuery.From.ID

		// Отвечаем на callback (убираем "часики")
		callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "")
		b.bot.Request(callbackConfig)
	} else {
		b.logger.Error().Msg("Error: cannot determine chatID and userID in handleSelectItem")
		return
	}

	// Обновляем активность пользователя
	b.updateUserActivity(userID)

	// Сохраняем состояние
	b.setUserState(ctx, userID, StateSelectItem, map[string]interface{}{
		"page": 0,
	})

	// Отправляем первую страницу
	b.sendItemsPage(ctx, chatID, userID, 0)
}

// sendItemsPage отправляет страницу с аппаратами
func (b *Bot) sendItemsPage(ctx context.Context, chatID, userID int64, page int) {
	itemsPerPage := 8 // Количество аппаратов на странице
	startIdx := page * itemsPerPage
	endIdx := startIdx + itemsPerPage
	if endIdx > len(b.items) {
		endIdx = len(b.items)
	}

	var message strings.Builder
	message.WriteString("🏢 *Доступные аппараты*\n\n")
	message.WriteString(fmt.Sprintf("Страница %d из %d\n\n", page+1, (len(b.items)+itemsPerPage-1)/itemsPerPage))

	// Текущие аппараты на странице
	currentItems := b.items[startIdx:endIdx]
	for i, item := range currentItems {
		message.WriteString(fmt.Sprintf("%d. *%s*\n", startIdx+i+1, item.Name))
		message.WriteString(fmt.Sprintf("   📝 %s\n", item.Description))
	}

	// Создаем Inline-клавиатуру
	var keyboard [][]tgbotapi.InlineKeyboardButton

	// Кнопки выбора аппаратов
	for i, item := range currentItems {
		btn := tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("%d. %s", startIdx+i+1, item.Name),
			fmt.Sprintf("select_item:%d", item.ID),
		)
		keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{btn})
	}

	// Кнопки навигации
	var navButtons []tgbotapi.InlineKeyboardButton

	if page > 0 {
		navButtons = append(navButtons, tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", fmt.Sprintf("items_page:%d", page-1)))
	}

	if endIdx < len(b.items) {
		navButtons = append(navButtons, tgbotapi.NewInlineKeyboardButtonData("Вперед ➡️", fmt.Sprintf("items_page:%d", page+1)))
	}

	if len(navButtons) > 0 {
		keyboard = append(keyboard, navButtons)
	}

	// Кнопка возврата
	keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад в меню", "back_to_main"),
	})

	markup := tgbotapi.NewInlineKeyboardMarkup(keyboard...)

	msg := tgbotapi.NewMessage(chatID, message.String())
	msg.ReplyMarkup = &markup
	msg.ParseMode = "Markdown"

	b.bot.Send(msg)
}

// showAvailableItems показывает доступные позиции
func (b *Bot) showAvailableItems(ctx context.Context, update tgbotapi.Update) {
	items := b.items
	var message strings.Builder
	message.WriteString("🏢 Доступные позиции:\n\n")

	for _, item := range items {
		message.WriteString(fmt.Sprintf("🔹 %s\n", item.Name))
		message.WriteString(fmt.Sprintf("   %s\n", item.Description))
		message.WriteString("\n")
	}

	var keyboard [][]tgbotapi.InlineKeyboardButton

	keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("📋 СОЗДАТЬ ЗАЯВКУ", "start_the_order"),
	})

	markup := tgbotapi.NewInlineKeyboardMarkup(keyboard...)

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, message.String())
	msg.ReplyMarkup = &markup

	b.bot.Send(msg)
}

// showMonthScheduleForItem показывает расписание на 30 дней для выбранного аппарата
func (b *Bot) showMonthScheduleForItem(ctx context.Context, update tgbotapi.Update) {
	state := b.getUserState(ctx, update.Message.From.ID)
	if state == nil || state.TempData["item_id"] == nil {
		b.sendMessage(update.Message.Chat.ID, "Ошибка: аппарат не выбран")
		return
	}

	itemID := b.getInt64FromTempData(state.TempData, "item_id")
	var selectedItem models.Item
	for _, item := range b.items {
		if item.ID == itemID {
			selectedItem = item
			break
		}
	}
	startDate := time.Now()

	availability, err := b.db.GetAvailabilityForPeriod(ctx, selectedItem.ID, startDate, 30)
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

	var keyboard [][]tgbotapi.InlineKeyboardButton

	keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("📋 СОЗДАТЬ ЗАЯВКУ НА ЭТОТ АППАРАТ", "start_the_order_item"),
	})

	markup := tgbotapi.NewInlineKeyboardMarkup(keyboard...)

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, message.String())
	msg.ReplyMarkup = &markup
	msg.ParseMode = "Markdown"
	b.bot.Send(msg)
}

// handleSpecificDateInput обновляем для работы с выбранным аппаратом
func (b *Bot) handleSpecificDateInput(ctx context.Context, update tgbotapi.Update, dateStr string) {
	state := b.getUserState(ctx, update.Message.From.ID)
	if state == nil || state.TempData["item_id"] == nil {
		b.sendMessage(update.Message.Chat.ID, "Ошибка: аппарат не выбран")
		return
	}

	itemID := b.getInt64FromTempData(state.TempData, "item_id")
	var selectedItem models.Item
	for _, item := range b.items {
		if item.ID == itemID {
			selectedItem = item
			break
		}
	}

	date, err := time.Parse("02.01.2006", dateStr)
	if err != nil {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID,
			"Неверный формат даты. Используйте ДД.ММ.ГГГГ (например, 25.12.2024)")
		b.bot.Send(msg)
		return
	}

	available, err := b.db.CheckAvailability(ctx, selectedItem.ID, date)
	if err != nil {
		b.logger.Error().Err(err).Int64("item_id", selectedItem.ID).Time("date", date).Msg("Error checking availability")
		b.sendMessage(update.Message.Chat.ID, "Ошибка при проверке доступности")
		return
	}

	status := "✅ Доступно"
	if !available {
		status = "❌ Недоступно"
	}

	booked, _ := b.db.GetBookedCount(ctx, selectedItem.ID, date)
	message := fmt.Sprintf("📅 Доступность *%s* на %s:\n\n%s\n\nЗабронировано: %d/%d",
		selectedItem.Name,
		date.Format("02.01.2006"),
		status,
		booked,
		selectedItem.TotalQuantity)

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, message)
	msg.ParseMode = "Markdown"
	b.bot.Send(msg)
}

// requestSpecificDate запрашивает у пользователя конкретную дату
func (b *Bot) requestSpecificDate(ctx context.Context, update tgbotapi.Update) {
	msg := tgbotapi.NewMessage(update.Message.Chat.ID,
		"Введите дату в формате ДД.ММ.ГГГГ (например, 25.12.2025):")

	b.setUserState(ctx, update.Message.From.ID, "waiting_specific_date", nil)
	b.bot.Send(msg)
}

// handleCustomInput ...
func (b *Bot) handleCustomInput(ctx context.Context, update tgbotapi.Update, state *models.UserState) {
	switch state.CurrentStep {
	default:
		b.sendMessage(update.Message.Chat.ID, "Неизвестная команда. Используйте меню.")
		b.handleMainMenu(ctx, update)
	}
}

// handleDateInput обрабатывает ввод даты для бронирования
func (b *Bot) handleDateInput(ctx context.Context, update tgbotapi.Update, dateStr string, state *models.UserState) {
	b.debugState(ctx, update.Message.From.ID, "handleDateInput START")

	date, err := time.Parse("02.01.2006", dateStr)
	if err != nil {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID,
			"Неверный формат даты. Используйте ДД.ММ.ГГГГ (например, 25.12.2024)")
		b.bot.Send(msg)
		return
	}

	// Проверяем, что дата не в прошлом
	if date.Before(time.Now().AddDate(0, 0, -1)) {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID,
			"Нельзя бронировать на прошедшие даты. Выберите будущую дату.")
		b.bot.Send(msg)
		return
	}

	itemID := b.getInt64FromTempData(state.TempData, "item_id")
	var item models.Item
	for _, it := range b.items {
		if it.ID == itemID {
			item = it
			break
		}
	}

	if item.ID == 0 {
		b.sendMessage(update.Message.Chat.ID, "Ошибка: не найден выбранный элемент. Начните заново.")
		b.handleMainMenu(ctx, update)
		return
	}

	// Проверяем доступность
	available, err := b.db.CheckAvailability(ctx, item.ID, date)
	if err != nil {
		b.logger.Error().Err(err).Int64("item_id", item.ID).Time("date", date).Msg("Error checking availability")
		msg := tgbotapi.NewMessage(update.Message.Chat.ID,
			"Произошла ошибка при проверке доступности. Попробуйте позже.")
		b.bot.Send(msg)
		return
	}

	if !available {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID,
			"К сожалению, на выбранную дату позиция недоступна. Выберите другую дату.")
		b.bot.Send(msg)
		return
	}

	// Сохраняем данные в состоянии перед переходом
	state.TempData["item_id"] = item.ID
	state.TempData["date"] = date
	b.setUserState(ctx, update.Message.From.ID, "waiting_date", state.TempData)

	b.debugState(ctx, update.Message.From.ID, "handleDateInput END")

	// Переходим к запросу персональных данных
	// b.handlePersonalData(ctx, update, item.ID, date)
	b.handleNameRequest(ctx, update)
}

// restoreStateOrRestart восстанавливает состояние или начинает заново
func (b *Bot) restoreStateOrRestart(ctx context.Context, update tgbotapi.Update, requiredFields ...string) bool {
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
func (b *Bot) handlePhoneReceived(ctx context.Context, update tgbotapi.Update, phone string) {
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
	itemID := state.TempData["item_id"].(int64)
	date := state.TempData["date"].(time.Time)

	// Находим выбранный элемент по ID
	var selectedItem models.Item
	for _, item := range b.items {
		if item.ID == itemID {
			selectedItem = item
			break
		}
	}

	if selectedItem.ID == 0 {
		b.sendMessage(update.Message.Chat.ID, "Ошибка: выбранная позиция не найдена. Начните заново.")
		b.handleMainMenu(ctx, update)
		return
	}

	state.TempData["phone"] = normalizedPhone
	state.TempData["item_id"] = selectedItem.ID // Сохраняем ID элемента для подтверждения
	b.setUserState(ctx, update.Message.From.ID, StateConfirmation, state.TempData)

	b.debugState(ctx, update.Message.From.ID, "handlePhoneReceived END")

	// Сохраняем телефон пользователя
	b.updateUserPhone(update.Message.From.ID, normalizedPhone)

	// Проверяем доступность еще раз
	available, err := b.db.CheckAvailability(ctx, selectedItem.ID, date)
	if err != nil || !available {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID,
			"К сожалению, выбранная позиция больше не доступна на эту дату. Пожалуйста, начните заново.")
		b.bot.Send(msg)
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

	// keyboard := tgbotapi.NewReplyKeyboard(
	// 	tgbotapi.NewKeyboardButtonRow(
	// 		tgbotapi.NewKeyboardButton("✅ Подтвердить заявку"),
	// 	),
	// 	tgbotapi.NewKeyboardButtonRow(
	// 		tgbotapi.NewKeyboardButton("❌ Отмена"),
	// 	),
	// )
	// msg.ReplyMarkup = keyboard

	b.bot.Send(msg)
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
