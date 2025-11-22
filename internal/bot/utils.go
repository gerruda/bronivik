package bot

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"bronivik/internal/models"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Вспомогательные методы для работы с состояниями пользователей

func (b *Bot) setUserState(userID int64, step string, tempData map[string]interface{}) {
	if tempData == nil {
		tempData = make(map[string]interface{})
	}

	b.userStates[userID] = &models.UserState{
		UserID:      userID,
		CurrentStep: step,
		TempData:    tempData,
	}
}

func (b *Bot) getUserState(userID int64) *models.UserState {
	return b.userStates[userID]
}

func (b *Bot) clearUserState(userID int64) {
	delete(b.userStates, userID)
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

// handleMainMenu - главное меню с контактами
func (b *Bot) handleMainMenu(update tgbotapi.Update) {
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
		log.Printf("Error: cannot determine userID and chatID in handleMainMenu")
		return
	}

	b.updateUserActivity(userID)

	msg := tgbotapi.NewMessage(chatID,
		"Добро пожаловать! Выберите действие:")

	var rows [][]tgbotapi.KeyboardButton

	// Основные кнопки для всех пользователей
	if !b.isManager(userID) {
		rows = append(rows, tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📅 Посмотреть расписание"),
			tgbotapi.NewKeyboardButton("💼 Доступные позиции"),
		))

		rows = append(rows, tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📋 Создать заявку"),
			tgbotapi.NewKeyboardButton("📞 Контакты менеджеров"),
		))

		rows = append(rows, tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📊 Мои заявки"),
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

	b.setUserState(userID, StateMainMenu, nil)
	b.bot.Send(msg)
}

// showManagerContacts показывает контакты менеджеров
func (b *Bot) showManagerContacts(update tgbotapi.Update) {
	contacts := b.config.ManagersContacts
	var message strings.Builder
	message.WriteString("📞 Контакты менеджеров:\n\n")
	for _, contact := range contacts {
		message.WriteString(fmt.Sprintf("🔹 %s\n", contact))
	}
	message.WriteString("\nВы можете связаться с ними для уточнения деталей.")

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, message.String())
	b.bot.Send(msg)
}

// showUserBookings показывает заявки пользователя
func (b *Bot) showUserBookings(update tgbotapi.Update) {
	bookings, err := b.db.GetUserBookings(context.Background(), update.Message.From.ID)
	if err != nil {
		log.Printf("Error getting user bookings: %v", err)
		b.sendMessage(update.Message.Chat.ID, "Ошибка при получении заявок")
		return
	}

	var message strings.Builder
	message.WriteString("📊 Ваши заявки:\n\n")

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
func (b *Bot) handlePersonalData(update tgbotapi.Update, itemID int64, date time.Time) {
	state := b.getUserState(update.Message.From.ID)
	if state == nil {
		state = &models.UserState{
			UserID:   update.Message.From.ID,
			TempData: make(map[string]interface{}),
		}
	}

	state.TempData["item_id"] = itemID
	state.TempData["date"] = date
	b.setUserState(update.Message.From.ID, StatePersonalData, state.TempData)

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
func (b *Bot) handleNameRequest(update tgbotapi.Update) {
	b.debugState(update.Message.From.ID, "handleNameRequest START")

	msg := tgbotapi.NewMessage(update.Message.Chat.ID,
		"Пожалуйста, введите ваше имя для заявки:")

	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("👤 Использовать имя из Telegram"),
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

	state := b.getUserState(update.Message.From.ID)

	b.setUserState(update.Message.From.ID, StateEnterName, state.TempData)

	b.debugState(update.Message.From.ID, "handleNameRequest END")
	b.bot.Send(msg)
}

// Обновляем handlePhoneRequest - добавляем контакты
func (b *Bot) handlePhoneRequest(update tgbotapi.Update) {
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

	state := b.getUserState(update.Message.From.ID)

	b.setUserState(update.Message.From.ID, StatePhoneNumber, state.TempData)
	b.bot.Send(msg)
}

// Обновляем finalizeBooking для использования имени
func (b *Bot) finalizeBooking(update tgbotapi.Update) {
	state := b.getUserState(update.Message.From.ID)
	if state == nil {
		b.sendMessage(update.Message.Chat.ID, "Сессия устарела. Начните заново.")
		b.handleMainMenu(update)
		return
	}

	// Получаем данные из состояния
	itemID := state.TempData["item_id"].(int64)
	date := state.TempData["date"].(time.Time)
	phone := state.TempData["phone"].(string)
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
		b.handleMainMenu(update)
		return
	}

	// Финальная проверка доступности
	available, err := b.db.CheckAvailability(context.Background(), selectedItem.ID, date)
	if err != nil || !available {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID,
			"К сожалению, выбранная позиция больше не доступна. Пожалуйста, выберите другую дату.")
		b.bot.Send(msg)
		b.handleMainMenu(update)
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

	err = b.db.CreateBooking(context.Background(), &booking)
	if err != nil {
		log.Printf("Error creating booking: %v", err)
		b.sendMessage(update.Message.Chat.ID, "Произошла ошибка при создании заявки. Попробуйте позже.")
		return
	}

	// Уведомляем менеджеров
	b.notifyManagers(booking)

	if b.sheetsService != nil {
		err := b.sheetsService.AppendBooking(&booking)
		if err != nil {
			log.Printf("Failed to sync booking to Google Sheets: %v", err)
			// Не прерываем выполнение, просто логируем ошибку
		} else {
			log.Printf("Booking synced to Google Sheets: %d", booking.ID)
		}
	}

	msg := tgbotapi.NewMessage(update.Message.Chat.ID,
		fmt.Sprintf("✅ Ваша заявка #%d успешно создана! Менеджер свяжется с вами для подтверждения.", booking.ID))

	go func() {
		time.Sleep(1 * time.Second) // Небольшая задержка для завершения операции в БД
		b.SyncBookingsToSheets()
		b.SyncScheduleToSheets()
	}()

	// Очищаем состояние
	b.clearUserState(update.Message.From.ID)
	b.handleMainMenu(update)
	b.bot.Send(msg)
}

// handleContactReceived обработка полученного контакта
func (b *Bot) handleContactReceived(update tgbotapi.Update) {
	state := b.getUserState(update.Message.From.ID)
	if state == nil {
		b.handleMainMenu(update)
		return
	}

	if state.CurrentStep == StatePhoneNumber {
		b.handlePhoneReceived(update, update.Message.Contact.PhoneNumber)
	}
}

// handleViewSchedule - меню просмотра расписания
func (b *Bot) handleViewSchedule(update tgbotapi.Update) {
	b.updateUserActivity(update.Message.From.ID)

	// Сохраняем состояние выбора аппарата для расписания
	b.setUserState(update.Message.From.ID, "schedule_select_item", map[string]interface{}{
		"page": 0,
	})

	// Отправляем выбор аппарата
	b.sendScheduleItemsPage(update.Message.Chat.ID, update.Message.From.ID, 0)
}

// sendScheduleItemsPage отправляет страницу с аппаратами для просмотра расписания
func (b *Bot) sendScheduleItemsPage(chatID, userID int64, page int) {
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

func (b *Bot) handleSelectItem(update tgbotapi.Update) {
	chatID := update.Message.Chat.ID
	userID := update.Message.From.ID

	// Сохраняем состояние
	b.setUserState(userID, StateSelectItem, map[string]interface{}{
		"page": 0,
	})

	// Отправляем первую страницу
	b.sendItemsPage(chatID, userID, 0)
}

// sendItemsPage отправляет страницу с аппаратами
func (b *Bot) sendItemsPage(chatID, userID int64, page int) {
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
func (b *Bot) showAvailableItems(update tgbotapi.Update) {
	items := b.items
	var message strings.Builder
	message.WriteString("🏢 Доступные позиции:\n\n")

	for _, item := range items {
		message.WriteString(fmt.Sprintf("🔹 %s\n", item.Name))
		message.WriteString(fmt.Sprintf("   %s\n", item.Description))
		message.WriteString("\n")
	}

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, message.String())
	b.bot.Send(msg)
}

// showWeekScheduleForItem показывает расписание на 7 дней для выбранного аппарата
func (b *Bot) showWeekScheduleForItem(update tgbotapi.Update) {
	state := b.getUserState(update.Message.From.ID)
	if state == nil || state.TempData["selected_item"] == nil {
		b.sendMessage(update.Message.Chat.ID, "Ошибка: аппарат не выбран")
		return
	}

	selectedItem := state.TempData["selected_item"].(models.Item)
	startDate := time.Now()

	var message strings.Builder
	message.WriteString(fmt.Sprintf("📅 Расписание *%s* на ближайшие 7 дней:\n\n", selectedItem.Name))

	availability, err := b.db.GetAvailabilityForPeriod(context.Background(), selectedItem.ID, startDate, 7)
	if err != nil {
		log.Printf("Error getting availability: %v", err)
		b.sendMessage(update.Message.Chat.ID, "Ошибка при получении расписания")
		return
	}

	for _, avail := range availability {
		status := "✅ Свободно"
		if avail.Available == 0 {
			status = "❌ Занято"
		}

		message.WriteString(fmt.Sprintf("   %s: %s\n",
			avail.Date.Format("02.01"), status))
	}

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, message.String())
	msg.ParseMode = "Markdown"
	b.bot.Send(msg)
}

// showMonthScheduleForItem показывает расписание на 30 дней для выбранного аппарата
func (b *Bot) showMonthScheduleForItem(update tgbotapi.Update) {
	state := b.getUserState(update.Message.From.ID)
	if state == nil || state.TempData["selected_item"] == nil {
		b.sendMessage(update.Message.Chat.ID, "Ошибка: аппарат не выбран")
		return
	}

	selectedItem := state.TempData["selected_item"].(models.Item)
	startDate := time.Now()

	var message strings.Builder
	message.WriteString(fmt.Sprintf("📅 Расписание *%s* на ближайшие 30 дней:\n\n", selectedItem.Name))

	availability, err := b.db.GetAvailabilityForPeriod(context.Background(), selectedItem.ID, startDate, 30)
	if err != nil {
		log.Printf("Error getting availability: %v", err)
		b.sendMessage(update.Message.Chat.ID, "Ошибка при получении расписания")
		return
	}

	for _, avail := range availability {
		status := "✅ Свободно"
		if avail.Available == 0 {
			status = "❌ Занято"
		}

		message.WriteString(fmt.Sprintf("   %s: %s\n",
			avail.Date.Format("02.01"), status))
	}

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, message.String())
	msg.ParseMode = "Markdown"
	b.bot.Send(msg)
}

// handleSpecificDateInput обновляем для работы с выбранным аппаратом
func (b *Bot) handleSpecificDateInput(update tgbotapi.Update, dateStr string) {
	state := b.getUserState(update.Message.From.ID)
	if state == nil || state.TempData["selected_item"] == nil {
		b.sendMessage(update.Message.Chat.ID, "Ошибка: аппарат не выбран")
		return
	}

	selectedItem := state.TempData["selected_item"].(models.Item)

	date, err := time.Parse("02.01.2006", dateStr)
	if err != nil {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID,
			"Неверный формат даты. Используйте ДД.ММ.ГГГГ (например, 25.12.2024)")
		b.bot.Send(msg)
		return
	}

	available, err := b.db.CheckAvailability(context.Background(), selectedItem.ID, date)
	if err != nil {
		log.Printf("Error checking availability: %v", err)
		b.sendMessage(update.Message.Chat.ID, "Ошибка при проверке доступности")
		return
	}

	status := "✅ Доступно"
	if !available {
		status = "❌ Недоступно"
	}

	booked, _ := b.db.GetBookedCount(context.Background(), selectedItem.ID, date)
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
func (b *Bot) requestSpecificDate(update tgbotapi.Update) {
	msg := tgbotapi.NewMessage(update.Message.Chat.ID,
		"Введите дату в формате ДД.ММ.ГГГГ (например, 25.12.2025):")

	b.setUserState(update.Message.From.ID, "waiting_specific_date", nil)
	b.bot.Send(msg)
}

// handleCustomInput ...
func (b *Bot) handleCustomInput(update tgbotapi.Update, state *models.UserState) {
	switch state.CurrentStep {
	default:
		b.sendMessage(update.Message.Chat.ID, "Неизвестная команда. Используйте меню.")
		b.handleMainMenu(update)
	}
}

// handleDateInput обрабатывает ввод даты для бронирования
func (b *Bot) handleDateInput(update tgbotapi.Update, dateStr string, state *models.UserState) {
	b.debugState(update.Message.From.ID, "handleDateInput START")

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

	item, ok := state.TempData["selected_item"].(models.Item)
	if !ok {
		b.sendMessage(update.Message.Chat.ID, "Ошибка: не найден выбранный элемент. Начните заново.")
		b.handleMainMenu(update)
		return
	}

	// Проверяем доступность
	available, err := b.db.CheckAvailability(context.Background(), item.ID, date)
	if err != nil {
		log.Printf("Error checking availability: %v", err)
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
	b.setUserState(update.Message.From.ID, "waiting_date", state.TempData)

	b.debugState(update.Message.From.ID, "handleDateInput END")

	// Переходим к запросу персональных данных
	// b.handlePersonalData(update, item.ID, date)
	b.handleNameRequest(update)
}

// restoreStateOrRestart восстанавливает состояние или начинает заново
func (b *Bot) restoreStateOrRestart(update tgbotapi.Update, requiredFields ...string) bool {
	state := b.getUserState(update.Message.From.ID)
	if state == nil {
		b.sendMessage(update.Message.Chat.ID, "Сессия устарела. Начните заново.")
		b.handleMainMenu(update)
		return false
	}

	for _, field := range requiredFields {
		if _, exists := state.TempData[field]; !exists {
			b.sendMessage(update.Message.Chat.ID,
				fmt.Sprintf("Ошибка: отсутствуют данные (%s). Начните заново.", field))
			b.handleMainMenu(update)
			return false
		}
	}

	return true
}

// Добавьте этот метод в utils.go для отладки
func (b *Bot) debugState(userID int64, message string) {
	state := b.getUserState(userID)
	if state != nil {
		log.Printf("DEBUG [%s] UserID: %d, Step: %s, TempData: %+v",
			message, userID, state.CurrentStep, state.TempData)
	} else {
		log.Printf("DEBUG [%s] UserID: %d, State: nil", message, userID)
	}
}

// handlePhoneReceived обработка полученного номера телефона
func (b *Bot) handlePhoneReceived(update tgbotapi.Update, phone string) {
	b.debugState(update.Message.From.ID, "handlePhoneReceived START")

	// Проверяем и восстанавливаем состояние
	if !b.restoreStateOrRestart(update, "item_id", "date") {
		return
	}

	state := b.getUserState(update.Message.From.ID)

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
		b.handleMainMenu(update)
		return
	}

	state.TempData["phone"] = normalizedPhone
	state.TempData["selected_item"] = selectedItem // Сохраняем элемент для подтверждения
	b.setUserState(update.Message.From.ID, StateConfirmation, state.TempData)

	b.debugState(update.Message.From.ID, "handlePhoneReceived END")

	// Сохраняем телефон пользователя
	b.updateUserPhone(update.Message.From.ID, normalizedPhone)

	// Проверяем доступность еще раз
	available, err := b.db.CheckAvailability(context.Background(), selectedItem.ID, date)
	if err != nil || !available {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID,
			"К сожалению, выбранная позиция больше не доступна на эту дату. Пожалуйста, начните заново.")
		b.bot.Send(msg)
		b.handleMainMenu(update)
		return
	}

	msg := tgbotapi.NewMessage(update.Message.Chat.ID,
		fmt.Sprintf(`📋 Подтверждение заявки:

🏢 Позиция: %s
📅 Дата: %s
👤 Имя: %s
📱 Телефон: %s`,
			selectedItem.Name,
			date.Format("02.01.2006"),
			update.Message.From.FirstName+" "+update.Message.From.LastName,
			normalizedPhone))

	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("✅ Подтвердить заявку"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("❌ Отмена"),
		),
	)
	msg.ReplyMarkup = keyboard

	b.bot.Send(msg)
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
