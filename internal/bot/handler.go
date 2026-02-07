package bot

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"bronivik/internal/config"
	"bronivik/internal/database"
	"bronivik/internal/google"
	"bronivik/internal/models"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Bot struct {
	bot           *tgbotapi.BotAPI
	config        *config.Config
	items         []models.Item
	db            *database.DB
	userStates    map[int64]*models.UserState
	sheetsService *google.SheetsService
}

func NewBot(token string, config *config.Config, items []models.Item, db *database.DB, googleService *google.SheetsService) (*Bot, error) {
	botAPI, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, err
	}

	return &Bot{
		bot:           botAPI,
		config:        config,
		items:         items,
		db:            db,
		userStates:    make(map[int64]*models.UserState),
		sheetsService: googleService,
	}, nil
}

const (
	StateMainMenu            = "main_menu"
	StateSelectItem          = "select_item"
	StateSelectDate          = "select_date"
	StateViewSchedule        = "view_schedule"
	StatePersonalData        = "personal_data"
	StateEnterName           = "enter_name"
	StatePhoneNumber         = "phone_number"
	StateConfirmation        = "confirmation"
	StateWaitingDate         = "waiting_date"
	StateWaitingSpecificDate = "waiting_specific_date"
)

func (b *Bot) Start() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.bot.GetUpdatesChan(u)

	log.Printf("Authorized on account %s", b.bot.Self.UserName)

	for update := range updates {
		if update.CallbackQuery != nil {
			b.handleCallbackQuery(update)
			continue
		}

		if update.Message == nil {
			continue
		}

		if b.isBlacklisted(update.Message.From.ID) {
			continue
		}

		b.handleMessage(update)
	}
}

func (b *Bot) handleMessage(update tgbotapi.Update) {
	userID := update.Message.From.ID
	text := update.Message.Text

	if b.isBlacklisted(userID) {
		return
	}

	if b.isManager(userID) {
		handled := b.handleManagerCommand(update)
		if handled {
			return // Если команда менеджера обработана, выходим
		}
	}

	state := b.getUserState(userID)

	switch {
	case text == "/start" || strings.ToLower(text) == "сброс" || strings.ToLower(text) == "reset":
		b.clearUserState(update.Message.From.ID)
		b.handleStartWithUserTracking(update)

	case text == "📞 Контакты менеджеров":
		b.showManagerContacts(update)

	case text == "📊 Мои заявки":
		b.showUserBookings(update)

	case text == "💼 Ассортимент":
		b.showAvailableItems(update)

	case text == "📅 Посмотреть расписание":
		b.handleViewSchedule(update)

	case text == "📋 СОЗДАТЬ ЗАЯВКУ":
		b.handleSelectItem(update)

	case text == "📅 30 дней":
		// Проверяем, есть ли выбранный аппарат для расписания
		state := b.getUserState(update.Message.From.ID)
		if state != nil && state.TempData["selected_item"] != nil {
			b.showMonthScheduleForItem(update)
		} else {
			// Если аппарат не выбран, просим выбрать сначала
			b.sendMessage(update.Message.Chat.ID, "Сначала выберите аппарат для просмотра расписания")
			b.handleViewSchedule(update)
		}

	case text == "🗓 Выбрать дату":
		// Проверяем, есть ли выбранный аппарат для расписания
		state := b.getUserState(update.Message.From.ID)
		if state != nil && state.TempData["selected_item"] != nil {
			b.requestSpecificDate(update)
		} else {
			b.sendMessage(update.Message.Chat.ID, "Сначала выберите аппарат для просмотра расписания")
			b.handleViewSchedule(update)
		}

	case text == "⬅️ Назад к выбору аппарата":
		b.handleViewSchedule(update)

	case text == "📋 СОЗДАТЬ ЗАЯВКУ НА ЭТОТ АППАРАТ":
		state := b.getUserState(update.Message.From.ID)
		if state != nil && state.TempData["selected_item"] != nil {
			selectedItem := state.TempData["selected_item"].(models.Item)
			// Сохраняем выбранный аппарат для создания заявки
			tempData := map[string]interface{}{
				"selected_item": selectedItem,
			}
			b.setUserState(update.Message.From.ID, StateWaitingDate, tempData)

			msg := tgbotapi.NewMessage(update.Message.Chat.ID,
				fmt.Sprintf("Вы выбрали: %s\n\nВведите дату бронирования в формате ДД.ММ.ГГГГ (например, 25.12.2024):",
					selectedItem.Name))

			keyboard := tgbotapi.NewReplyKeyboard(
				tgbotapi.NewKeyboardButtonRow(
					tgbotapi.NewKeyboardButton("⬅️ Назад"),
				),
			)
			msg.ReplyMarkup = keyboard
			b.bot.Send(msg)
		}

	case text == "⬅️ Назад":
		if state != nil {
			// Возвращаемся к предыдущему шагу в зависимости от текущего состояния
			switch state.CurrentStep {
			case StateEnterName:
				b.handleMainMenu(update)
			case StatePhoneNumber:
				b.handleNameRequest(update)
			case StateConfirmation:
				b.handlePhoneRequest(update)
			default:
				b.handleMainMenu(update)
			}
		} else {
			b.handleMainMenu(update)
		}

	case text == "⬅️ Назад в меню":
		b.clearUserState(update.Message.From.ID)
		b.handleMainMenu(update)

	case state != nil && state.CurrentStep == StatePersonalData && text == "✅ Даю согласие":
		b.handleNameRequest(update)

	case state != nil && state.CurrentStep == StateEnterName:
		if text == "👤 Использовать имя из Telegram" {
			// Используем имя из Telegram
			state.TempData["user_name"] = update.Message.From.FirstName + " " + update.Message.From.LastName
			b.setUserState(update.Message.From.ID, StatePhoneNumber, state.TempData)
			b.handlePhoneRequest(update)
		} else if text == "📞 Контакты менеджеров" {
			b.showManagerContacts(update)
		} else if text == "❌ Отмена" {
			b.clearUserState(update.Message.From.ID)
			b.handleMainMenu(update)
		} else {
			// Сохраняем введенное имя
			if len(text) < 2 {
				b.sendMessage(update.Message.Chat.ID, "Имя слишком короткое. Введите имя длиной от 2 символов.")
				return
			}
			if len(text) > 150 {
				b.sendMessage(update.Message.Chat.ID, "Имя слишком длинное. Введите имя до 150 символов.")
				return
			}
			state.TempData["user_name"] = text
			b.setUserState(update.Message.From.ID, StatePhoneNumber, state.TempData)
			b.handlePhoneRequest(update)
		}

	case state != nil && state.CurrentStep == StatePhoneNumber:
		if update.Message.Contact != nil {
			b.handleContactReceived(update)
		} else {
			b.handlePhoneReceived(update, text)
		}

	case state != nil && state.CurrentStep == StateConfirmation && text == "✅ Подтвердить заявку":
		b.finalizeBooking(update)

	case state != nil && state.CurrentStep == StateWaitingDate:
		b.handleDateInput(update, text, state)

	case state != nil && state.CurrentStep == StateWaitingSpecificDate:
		b.handleSpecificDateInput(update, text)

	case text == "❌ Отмена":
		b.clearUserState(update.Message.From.ID)
		b.handleMainMenu(update)
	}
}

// handleCallbackQuery обработка callback запросов от inline кнопок
func (b *Bot) handleCallbackQuery(update tgbotapi.Update) {
	callback := update.CallbackQuery
	if callback == nil {
		return
	}

	// Проверка черного списка
	if b.isBlacklisted(callback.From.ID) {
		return
	}

	data := callback.Data

	switch {
	case data == "export_users":
		b.handleExportUsers(update)

	case strings.HasPrefix(data, "confirm_"),
		strings.HasPrefix(data, "reject_"),
		strings.HasPrefix(data, "reschedule_"),
		strings.HasPrefix(data, "change_item_"),
		strings.HasPrefix(data, "reopen_"),
		strings.HasPrefix(data, "complete_"):
		b.handleManagerAction(update)

	case strings.HasPrefix(data, "change_to_"):
		b.handleChangeItem(update)

	case strings.HasPrefix(data, "select_item:"):
		b.handleItemSelectionFromCallback(update)

	case strings.HasPrefix(data, "items_page:"):
		pageStr := strings.TrimPrefix(data, "items_page:")
		page, err := strconv.Atoi(pageStr)
		if err != nil {
			log.Printf("Error parsing page: %v", err)
			return
		}

		// Редактируем сообщение с новой страницей
		b.editItemsPage(update, page)

	case strings.HasPrefix(data, "schedule_select_item:"):
		b.handleScheduleItemSelection(update)

	case strings.HasPrefix(data, "schedule_items_page:"):
		pageStr := strings.TrimPrefix(data, "schedule_items_page:")
		page, err := strconv.Atoi(pageStr)
		if err != nil {
			log.Printf("Error parsing page: %v", err)
			return
		}
		b.editScheduleItemsPage(update, page)

	case strings.HasPrefix(data, "back_to_main"):
		b.clearUserState(callback.From.ID)
		tempUpdate := tgbotapi.Update{
			CallbackQuery: callback,
		}
		b.handleMainMenu(tempUpdate)

	case strings.HasPrefix(data, "call_booking"):
		b.handleCallButton(update)

	case strings.HasPrefix(data, "show_booking:"):
		parts := strings.Split(data, ":")
		if len(parts) >= 2 {
			bookingID, err := strconv.ParseInt(parts[1], 10, 64)
			if err == nil {
				// Получаем заявку и показываем детали
				booking, err := b.db.GetBooking(context.Background(), bookingID)
				if err == nil {
					b.sendManagerBookingDetail(callback.Message.Chat.ID, booking)
				}
			}
		}

	case data == "start_the_order":
		b.handleSelectItem(update)

	case data == "start_the_order_item":
		state := b.getUserState(callback.From.ID)
		if state != nil && state.TempData["selected_item"] != nil {
			selectedItem := state.TempData["selected_item"].(models.Item)
			// Сохраняем выбранный аппарат для создания заявки
			tempData := map[string]interface{}{
				"selected_item": selectedItem,
			}
			b.setUserState(callback.From.ID, StateWaitingDate, tempData)

			msg := tgbotapi.NewMessage(callback.Message.Chat.ID,
				fmt.Sprintf("Вы выбрали: %s\n\nВведите дату бронирования в формате ДД.ММ.ГГГГ (например, 25.12.2024):",
					selectedItem.Name))

			keyboard := tgbotapi.NewReplyKeyboard(
				tgbotapi.NewKeyboardButtonRow(
					tgbotapi.NewKeyboardButton("⬅️ Назад"),
				),
			)
			msg.ReplyMarkup = keyboard
			b.bot.Send(msg)
		}

	default:
		log.Printf("Unknown callback data: %s", callback.Data)
	}

	// Обработка выбора аппарата менеджером
	if strings.HasPrefix(data, "manager_select_item:") {
		b.handleManagerItemSelection(update)
	} else if strings.HasPrefix(data, "manager_items_page:") {
		pageStr := strings.TrimPrefix(data, "manager_items_page:")
		page, err := strconv.Atoi(pageStr)
		if err != nil {
			log.Printf("Error parsing page: %v", err)
			return
		}
		b.editManagerItemsPage(update, page)
	} else if data == "manager_single_date" {
		b.handleManagerDateType(update, "single")
	} else if data == "manager_date_range" {
		b.handleManagerDateType(update, "range")
	}

	// Ответ на callback (убирает "часики" на кнопке)
	b.bot.Send(tgbotapi.NewCallback(callback.ID, ""))
}

// handleScheduleItemSelection обработка выбора аппарата для расписания
func (b *Bot) handleScheduleItemSelection(update tgbotapi.Update) {
	callback := update.CallbackQuery
	data := callback.Data

	b.debugState(callback.Message.Chat.ID, "DEBUG: handleScheduleItemSelection START")

	itemIDStr := strings.TrimPrefix(data, "schedule_select_item:")
	itemID, err := strconv.ParseInt(itemIDStr, 10, 64)
	if err != nil {
		log.Printf("Error parsing item ID: %v", err)
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

	// Сохраняем выбранный аппарат в состоянии
	b.setUserState(callback.From.ID, "schedule_view_menu", map[string]interface{}{
		"selected_item": selectedItem,
	})

	// Редактируем сообщение, убирая клавиатуру
	editMsg := tgbotapi.NewEditMessageText(
		callback.Message.Chat.ID,
		callback.Message.MessageID,
		fmt.Sprintf("✅ Вы выбрали: *%s*\n\nТеперь выберите период для просмотра расписания:", selectedItem.Name),
	)
	editMsg.ParseMode = "Markdown"
	b.bot.Send(editMsg)

	// Отправляем меню расписания для выбранного аппарата
	b.sendScheduleMenu(callback.Message.Chat.ID, callback.From.ID)
}

// sendScheduleMenu показывает меню расписания для выбранного аппарата
func (b *Bot) sendScheduleMenu(chatID, userID int64) {
	state := b.getUserState(userID)
	if state == nil || state.TempData["selected_item"] == nil {
		b.sendMessage(chatID, "Ошибка: аппарат не выбран")
		return
	}

	selectedItem := state.TempData["selected_item"].(models.Item)

	msg := tgbotapi.NewMessage(chatID,
		fmt.Sprintf("📅 *Расписание для %s*\n\nВыберите период:", selectedItem.Name))

	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📋 СОЗДАТЬ ЗАЯВКУ НА ЭТОТ АППАРАТ"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📅 30 дней"),
			tgbotapi.NewKeyboardButton("🗓 Выбрать дату"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("⬅️ Назад к выбору аппарата"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("⬅️ Назад в меню"),
		),
	)
	msg.ReplyMarkup = keyboard
	msg.ParseMode = "Markdown"

	b.bot.Send(msg)
}

// editScheduleItemsPage редактирует страницу с аппаратами для расписания
func (b *Bot) editScheduleItemsPage(update tgbotapi.Update, page int) {
	callback := update.CallbackQuery
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

	for i, item := range currentItems {
		btn := tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("%d. %s", startIdx+i+1, item.Name),
			fmt.Sprintf("schedule_select_item:%d", item.ID),
		)
		keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{btn})
	}

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

	keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад в меню", "back_to_main_from_schedule"),
	})

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

// handleItemSelectionFromCallback обработка выбора аппарата из Inline-клавиатуры
func (b *Bot) handleItemSelectionFromCallback(update tgbotapi.Update) {
	callback := update.CallbackQuery
	data := callback.Data

	itemIDStr := strings.TrimPrefix(data, "select_item:")
	itemID, err := strconv.ParseInt(itemIDStr, 10, 64)
	if err != nil {
		log.Printf("Error parsing item ID: %v", err)
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

	// Сохраняем в состоянии
	state := b.getUserState(callback.From.ID)
	if state == nil {
		state = &models.UserState{
			UserID:   callback.From.ID,
			TempData: make(map[string]interface{}),
		}
	}
	state.TempData["selected_item"] = selectedItem
	b.setUserState(callback.From.ID, StateWaitingDate, state.TempData)

	// Редактируем сообщение, убирая клавиатуру
	editMsg := tgbotapi.NewEditMessageText(
		callback.Message.Chat.ID,
		callback.Message.MessageID,
		fmt.Sprintf("✅ Вы выбрали: *%s*\n\nВведите дату бронирования в формате ДД.ММ.ГГГГ (например, 25.12.2024):", selectedItem.Name),
	)
	editMsg.ParseMode = "Markdown"
	b.bot.Send(editMsg)

	// Отправляем кнопку "Назад"
	msg := tgbotapi.NewMessage(callback.Message.Chat.ID, "Или используйте кнопку ниже:")
	msg.ReplyMarkup = tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("⬅️ Назад"),
		),
	)
	b.bot.Send(msg)

	b.bot.Send(tgbotapi.NewCallback(callback.ID, fmt.Sprintf("Выбрано: %s", selectedItem.Name)))
}

// editItemsPage редактирует сообщение с новой страницей аппаратов
func (b *Bot) editItemsPage(update tgbotapi.Update, page int) {
	callback := update.CallbackQuery
	itemsPerPage := 8
	startIdx := page * itemsPerPage
	endIdx := startIdx + itemsPerPage
	if endIdx > len(b.items) {
		endIdx = len(b.items)
	}

	var message strings.Builder
	message.WriteString("🏢 *Доступные аппараты*\n\n")
	message.WriteString(fmt.Sprintf("Страница %d из %d\n\n", page+1, (len(b.items)+itemsPerPage-1)/itemsPerPage))

	currentItems := b.items[startIdx:endIdx]
	for i, item := range currentItems {
		message.WriteString(fmt.Sprintf("%d. *%s*\n", startIdx+i+1, item.Name))
		message.WriteString(fmt.Sprintf("   📝 %s\n", item.Description))
	}

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

	keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад в меню", "back_to_main"),
	})

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

// saveUser сохраняет/обновляет информацию о пользователе
func (b *Bot) saveUser(update tgbotapi.Update) {
	user := update.Message.From
	if user == nil {
		return
	}

	// Проверяем, является ли пользователь менеджером или в черном списке
	isManager := b.isManager(user.ID)
	isBlacklisted := b.isBlacklisted(user.ID)

	// Создаем модель пользователя
	dbUser := &models.User{
		TelegramID:    user.ID,
		Username:      user.UserName,
		FirstName:     user.FirstName,
		LastName:      user.LastName,
		Phone:         "", // Телефон будет обновлен позже, если пользователь его предоставит
		IsManager:     isManager,
		IsBlacklisted: isBlacklisted,
		LanguageCode:  user.LanguageCode,
		LastActivity:  time.Now(),
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	// Сохраняем в базу данных
	err := b.db.CreateOrUpdateUser(context.Background(), dbUser)
	if err != nil {
		log.Printf("Ошибка при сохранении пользователя %d: %v", user.ID, err)
	} else {
		log.Printf("Пользователь сохранен: %s (ID: %d)", user.FirstName, user.ID)
	}

	b.SyncUsersToSheets()
}

// updateUserPhone обновляет номер телефона пользователя
func (b *Bot) updateUserPhone(telegramID int64, phone string) {
	err := b.db.UpdateUserPhone(context.Background(), telegramID, phone)
	if err != nil {
		log.Printf("Ошибка при обновлении телефона пользователя %d: %v", telegramID, err)
	} else {
		log.Printf("Телефон обновлен для пользователя %d", telegramID)
	}
}

// updateUserActivity обновляет время последней активности пользователя
func (b *Bot) updateUserActivity(telegramID int64) {
	err := b.db.UpdateUserActivity(context.Background(), telegramID)
	if err != nil {
		log.Printf("Ошибка при обновлении активности пользователя %d: %v", telegramID, err)
	}
}

// handleStartWithUserTracking обработка /start с сохранением пользователя
func (b *Bot) handleStartWithUserTracking(update tgbotapi.Update) {
	// Сохраняем пользователя
	b.saveUser(update)

	// Обновляем активность
	b.updateUserActivity(update.Message.From.ID)

	// Показываем главное меню
	b.handleMainMenu(update)
}

// getUserStats возвращает статистику пользователей (для менеджеров)
func (b *Bot) getUserStats(update tgbotapi.Update) {
	if !b.isManager(update.Message.From.ID) {
		return
	}

	ctx := context.Background()

	// Получаем общую статистику
	allUsers, err := b.db.GetAllUsers(ctx)
	if err != nil {
		log.Printf("Error getting users: %v", err)
		b.sendMessage(update.Message.Chat.ID, "Ошибка при получении статистики")
		return
	}

	activeUsers, err := b.db.GetActiveUsers(ctx, 30) // Активные за последние 30 дней
	if err != nil {
		log.Printf("Error getting active users: %v", err)
	}

	managers, err := b.db.GetUsersByManagerStatus(ctx, true)
	if err != nil {
		log.Printf("Error getting managers: %v", err)
	}

	_, err = b.db.GetUsersByManagerStatus(ctx, false) // Черный список - это не менеджеры с is_blacklisted = true
	// Нужно отдельно считать черный список
	var blacklistedCount int
	for _, user := range allUsers {
		if user.IsBlacklisted {
			blacklistedCount++
		}
	}

	// Формируем сообщение со статистикой
	var message strings.Builder
	message.WriteString("📊 *Статистика пользователей*\n\n")
	message.WriteString(fmt.Sprintf("👥 Всего пользователей: *%d*\n", len(allUsers)))
	message.WriteString(fmt.Sprintf("🟢 Активных (30 дней): *%d*\n", len(activeUsers)))
	message.WriteString(fmt.Sprintf("👨‍💼 Менеджеров: *%d*\n", len(managers)))
	message.WriteString(fmt.Sprintf("🚫 В черном списке: *%d*\n\n", blacklistedCount))

	// Последние 5 пользователей
	message.WriteString("📈 *Последние пользователи:*\n")
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

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, message.String())
	msg.ParseMode = "Markdown"

	// Добавляем кнопку для экспорта пользователей
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📤 Экспорт пользователей", "export_users"),
		),
	)
	msg.ReplyMarkup = &keyboard

	b.bot.Send(msg)
}

// handleExportUsers обработка экспорта пользователей
func (b *Bot) handleExportUsers(update tgbotapi.Update) {
	callback := update.CallbackQuery
	if callback == nil || !b.isManager(callback.From.ID) {
		return
	}

	users, err := b.db.GetAllUsers(context.Background())
	if err != nil {
		log.Printf("Error getting users for export: %v", err)
		b.sendMessage(callback.Message.Chat.ID, "Ошибка при получении данных пользователей")
		return
	}

	filePath, err := b.exportUsersToExcel(users)
	if err != nil {
		log.Printf("Error exporting users to Excel: %v", err)
		b.sendMessage(callback.Message.Chat.ID, "Ошибка при создании файла экспорта")
		return
	}

	// Отправляем файл
	file, err := os.Open(filePath)
	if err != nil {
		log.Printf("Error opening file: %v", err)
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
		log.Printf("Error sending document: %v", err)
		b.sendMessage(callback.Message.Chat.ID, "Ошибка при отправке файла")
		return
	}

	b.sendMessage(callback.Message.Chat.ID, "✅ Файл с пользователями успешно отправлен")
}

// SyncUsersToSheets синхронизирует пользователей с Google Sheets
func (b *Bot) SyncUsersToSheets() {
	if b.sheetsService == nil {
		return
	}

	users, err := b.db.GetAllUsers(context.Background())
	if err != nil {
		log.Printf("Failed to get users for Google Sheets sync: %v", err)
		return
	}

	var googleUsers []*models.User
	for _, user := range users {
		googleUsers = append(googleUsers, &models.User{
			ID:            user.ID,
			TelegramID:    user.TelegramID,
			Username:      user.Username,
			FirstName:     user.FirstName,
			LastName:      user.LastName,
			Phone:         user.Phone,
			IsManager:     user.IsManager,
			IsBlacklisted: user.IsBlacklisted,
			LanguageCode:  user.LanguageCode,
			LastActivity:  user.LastActivity,
			CreatedAt:     user.CreatedAt,
			UpdatedAt:     user.UpdatedAt,
		})
	}

	err = b.sheetsService.UpdateUsersSheet(googleUsers)
	if err != nil {
		log.Printf("Failed to sync users to Google Sheets: %v", err)
	} else {
		log.Println("Users successfully synced to Google Sheets")
	}
}

// SyncBookingsToSheets синхронизирует бронирования с Google Sheets
func (b *Bot) SyncBookingsToSheets() {
	if b.sheetsService == nil {
		log.Println("Google Sheets service not initialized")
		return
	}

	// Получаем бронирования за период: один месяц назад и два месяца вперед
	startDate := time.Now().AddDate(0, -1, 0) // 1 месяц назад
	endDate := time.Now().AddDate(0, 2, 0)    // 2 месяца вперед

	bookings, err := b.db.GetBookingsByDateRange(context.Background(), startDate, endDate)
	if err != nil {
		log.Printf("Failed to get bookings for Google Sheets sync: %v", err)
		return
	}

	log.Printf("Syncing %d bookings to Google Sheets", len(bookings))

	// Конвертируем в модели для Google Sheets
	var googleBookings []*models.Booking
	for _, booking := range bookings {
		googleBookings = append(googleBookings, &models.Booking{
			ID:           booking.ID,
			UserID:       booking.UserID,
			ItemID:       booking.ItemID,
			Date:         booking.Date,
			Status:       booking.Status,
			UserName:     booking.UserName,
			Phone:        booking.Phone,
			ItemName:     booking.ItemName,
			Comment:      booking.Comment,
			UserNickname: booking.UserNickname,
			CreatedAt:    booking.CreatedAt,
			UpdatedAt:    booking.UpdatedAt,
		})
	}

	// Полностью перезаписываем лист с заявками
	err = b.sheetsService.ReplaceBookingsSheet(googleBookings)
	if err != nil {
		log.Printf("Failed to sync bookings to Google Sheets: %v", err)
	} else {
		log.Printf("Bookings successfully synced to Google Sheets: %d records", len(googleBookings))
	}

	// Также синхронизируем расписание
	b.SyncScheduleToSheets()
}

// AppendBookingToSheets добавляет одно бронирование в Google Sheets
func (b *Bot) AppendBookingToSheets(booking *models.Booking) {
	if b.sheetsService == nil {
		return
	}

	googleBooking := &models.Booking{
		ID:        booking.ID,
		UserID:    booking.UserID,
		ItemID:    booking.ItemID,
		Date:      booking.Date,
		Status:    booking.Status,
		UserName:  booking.UserName,
		Phone:     booking.Phone,
		ItemName:  booking.ItemName,
		CreatedAt: booking.CreatedAt,
		UpdatedAt: booking.UpdatedAt,
	}

	err := b.sheetsService.AppendBooking(googleBooking)
	if err != nil {
		log.Printf("Failed to append booking to Google Sheets: %v", err)
	} else {
		log.Printf("Booking %d appended to Google Sheets", booking.ID)
	}
}
