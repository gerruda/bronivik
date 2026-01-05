package bot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"bronivik/internal/config"
	"bronivik/internal/database"
	"bronivik/internal/events"
	"bronivik/internal/google"
	"bronivik/internal/models"
	"bronivik/internal/service"
	"bronivik/internal/worker"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type Bot struct {
	bot           *tgbotapi.BotAPI
	config        *config.Config
	items         []models.Item
	db            *database.DB
	stateService  *service.StateService
	sheetsService *google.SheetsService
	sheetsWorker  *worker.SheetsWorker
	eventBus      *events.EventBus
	logger        *zerolog.Logger
}

func NewBot(token string, config *config.Config, items []models.Item, db *database.DB, stateService *service.StateService, googleService *google.SheetsService, sheetsWorker *worker.SheetsWorker, eventBus *events.EventBus, logger *zerolog.Logger) (*Bot, error) {
	botAPI, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, err
	}

	if eventBus == nil {
		eventBus = events.NewEventBus()
	}

	if logger == nil {
		l := zerolog.New(os.Stdout).With().Timestamp().Logger()
		logger = &l
	}

	return &Bot{
		bot:           botAPI,
		config:        config,
		items:         items,
		db:            db,
		stateService:  stateService,
		sheetsService: googleService,
		sheetsWorker:  sheetsWorker,
		eventBus:      eventBus,
		logger:        logger,
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

func (b *Bot) Start(ctx context.Context) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.bot.GetUpdatesChan(u)

	b.logger.Info().Str("username", b.bot.Self.UserName).Msg("Authorized on account")

	for {
		select {
		case <-ctx.Done():
			b.logger.Info().Msg("Bot stopping...")
			return
		case update, ok := <-updates:
			if !ok {
				return
			}
			// Создаем контекст для обработки каждого обновления
			updateCtx, cancel := context.WithTimeout(ctx, 30*time.Second)

			requestID := uuid.New().String()
			l := b.logger.With().Str("request_id", requestID).Logger()
			updateCtx = l.WithContext(updateCtx)
			
			if update.CallbackQuery != nil {
				b.handleCallbackQuery(updateCtx, update)
				cancel()
				continue
			}

			if update.Message == nil {
				cancel()
				continue
			}

			if b.isBlacklisted(update.Message.From.ID) {
				cancel()
				continue
			}

			b.handleMessage(updateCtx, update)
			cancel()
		}
	}
}

func (b *Bot) handleMessage(ctx context.Context, update tgbotapi.Update) {
	userID := update.Message.From.ID
	text := update.Message.Text
	l := zerolog.Ctx(ctx)

	l.Debug().
		Int64("user_id", userID).
		Str("username", update.Message.From.UserName).
		Str("text", text).
		Msg("Handling message")

	if b.isBlacklisted(userID) {
		return
	}

	if b.isManager(userID) {
		handled := b.handleManagerCommand(ctx, update)
		if handled {
			return // Если команда менеджера обработана, выходим
		}
	}

	state := b.getUserState(ctx, userID)

	switch {
	case text == "/start" || strings.ToLower(text) == "сброс" || strings.ToLower(text) == "reset":
		b.clearUserState(ctx, update.Message.From.ID)
		b.handleStartWithUserTracking(ctx, update)

	case text == "📞 Контакты менеджеров":
		b.showManagerContacts(ctx, update)

	case text == "📊 Мои заявки":
		b.showUserBookings(ctx, update)

	case text == "💼 Ассортимент":
		b.showAvailableItems(ctx, update)

	case text == "📅 Посмотреть расписание":
		b.handleViewSchedule(ctx, update)

	case text == "📋 СОЗДАТЬ ЗАЯВКУ":
		b.handleSelectItem(ctx, update)

	case text == "📅 30 дней":
		// Проверяем, есть ли выбранный аппарат для расписания
		state := b.getUserState(ctx, update.Message.From.ID)
		if state != nil && state.TempData["item_id"] != nil {
			b.showMonthScheduleForItem(ctx, update)
		} else {
			// Если аппарат не выбран, просим выбрать сначала
			b.sendMessage(update.Message.Chat.ID, "Сначала выберите аппарат для просмотра расписания")
			b.handleViewSchedule(ctx, update)
		}

	case text == "🗓 Выбрать дату":
		// Проверяем, есть ли выбранный аппарат для расписания
		state := b.getUserState(ctx, update.Message.From.ID)
		if state != nil && state.TempData["item_id"] != nil {
			b.requestSpecificDate(ctx, update)
		} else {
			b.sendMessage(update.Message.Chat.ID, "Сначала выберите аппарат для просмотра расписания")
			b.handleViewSchedule(ctx, update)
		}

	case text == "⬅️ Назад к выбору аппарата":
		b.handleViewSchedule(ctx, update)

	case text == "📋 СОЗДАТЬ ЗАЯВКУ НА ЭТОТ АППАРАТ":
		state := b.getUserState(ctx, update.Message.From.ID)
		if state != nil && state.TempData["item_id"] != nil {
			itemID := b.getInt64FromTempData(state.TempData, "item_id")
			var selectedItem models.Item
			for _, item := range b.items {
				if item.ID == itemID {
					selectedItem = item
					break
				}
			}
			// Сохраняем выбранный аппарат для создания заявки
			tempData := map[string]interface{}{
				"item_id": selectedItem.ID,
			}
			b.setUserState(ctx, update.Message.From.ID, StateWaitingDate, tempData)

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
				b.handleMainMenu(ctx, update)
			case StatePhoneNumber:
				b.handleNameRequest(ctx, update)
			case StateConfirmation:
				b.handlePhoneRequest(ctx, update)
			default:
				b.handleMainMenu(ctx, update)
			}
		} else {
			b.handleMainMenu(ctx, update)
		}

	case text == "⬅️ Назад в меню":
		b.clearUserState(ctx, update.Message.From.ID)
		b.handleMainMenu(ctx, update)

	case state != nil && state.CurrentStep == StatePersonalData && text == "✅ Даю согласие":
		b.handleNameRequest(ctx, update)

	case state != nil && state.CurrentStep == StateEnterName:
		if text == "👤 Использовать имя из Telegram" {
			// Используем имя из Telegram
			state.TempData["user_name"] = update.Message.From.FirstName + " " + update.Message.From.LastName
			b.setUserState(ctx, update.Message.From.ID, StatePhoneNumber, state.TempData)
			b.handlePhoneRequest(ctx, update)
		} else if text == "📞 Контакты менеджеров" {
			b.showManagerContacts(ctx, update)
		} else if text == "❌ Отмена" {
			b.clearUserState(ctx, update.Message.From.ID)
			b.handleMainMenu(ctx, update)
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
			b.setUserState(ctx, update.Message.From.ID, StatePhoneNumber, state.TempData)
			b.handlePhoneRequest(ctx, update)
		}

	case state != nil && state.CurrentStep == StatePhoneNumber:
		if update.Message.Contact != nil {
			b.handleContactReceived(ctx, update)
		} else {
			b.handlePhoneReceived(ctx, update, text)
		}

	case state != nil && state.CurrentStep == StateConfirmation && text == "✅ Подтвердить заявку":
		b.finalizeBooking(ctx, update)

	case state != nil && state.CurrentStep == StateWaitingDate:
		b.handleDateInput(ctx, update, text, state)

	case state != nil && state.CurrentStep == StateWaitingSpecificDate:
		b.handleSpecificDateInput(ctx, update, text)

	case text == "❌ Отмена":
		b.clearUserState(ctx, update.Message.From.ID)
		b.handleMainMenu(ctx, update)
	}
}

// handleCallbackQuery обработка callback запросов от inline кнопок
func (b *Bot) handleCallbackQuery(ctx context.Context, update tgbotapi.Update) {
	callback := update.CallbackQuery
	if callback == nil {
		return
	}

	l := zerolog.Ctx(ctx)
	l.Debug().
		Int64("user_id", callback.From.ID).
		Str("data", callback.Data).
		Msg("Handling callback query")

	// Проверка черного списка
	if b.isBlacklisted(callback.From.ID) {
		return
	}

	data := callback.Data

	switch {
	case data == "export_users":
		b.handleExportUsers(ctx, update)

	case strings.HasPrefix(data, "confirm_"),
		strings.HasPrefix(data, "reject_"),
		strings.HasPrefix(data, "reschedule_"),
		strings.HasPrefix(data, "change_item_"),
		strings.HasPrefix(data, "reopen_"),
		strings.HasPrefix(data, "complete_"):
		b.handleManagerAction(ctx, update)

	case strings.HasPrefix(data, "change_to_"):
		b.handleChangeItem(ctx, update)

	case strings.HasPrefix(data, "select_item:"):
		b.handleItemSelectionFromCallback(ctx, update)

	case strings.HasPrefix(data, "items_page:"):
		pageStr := strings.TrimPrefix(data, "items_page:")
		page, err := strconv.Atoi(pageStr)
		if err != nil {
			b.logger.Error().Err(err).Str("page_str", pageStr).Msg("Error parsing page")
			return
		}

		// Редактируем сообщение с новой страницей
		b.editItemsPage(ctx, update, page)

	case strings.HasPrefix(data, "schedule_select_item:"):
		b.handleScheduleItemSelection(ctx, update)

	case strings.HasPrefix(data, "schedule_items_page:"):
		pageStr := strings.TrimPrefix(data, "schedule_items_page:")
		page, err := strconv.Atoi(pageStr)
		if err != nil {
			b.logger.Error().Err(err).Str("page_str", pageStr).Msg("Error parsing page")
			return
		}
		b.editScheduleItemsPage(ctx, update, page)

	case strings.HasPrefix(data, "back_to_main"):
		b.clearUserState(ctx, callback.From.ID)
		tempUpdate := tgbotapi.Update{
			CallbackQuery: callback,
		}
		b.handleMainMenu(ctx, tempUpdate)

	case strings.HasPrefix(data, "call_booking"):
		b.handleCallButton(ctx, update)

	case strings.HasPrefix(data, "show_booking:"):
		parts := strings.Split(data, ":")
		if len(parts) >= 2 {
			bookingID, err := strconv.ParseInt(parts[1], 10, 64)
			if err == nil {
				// Получаем заявку и показываем детали
				booking, err := b.db.GetBooking(ctx, bookingID)
				if err == nil {
					b.sendManagerBookingDetail(ctx, callback.Message.Chat.ID, booking)
				}
			}
		}

	case data == "start_the_order":
		b.handleSelectItem(ctx, update)

	case data == "start_the_order_item":
		state := b.getUserState(ctx, callback.From.ID)
		if state != nil && state.TempData["item_id"] != nil {
			itemID := b.getInt64FromTempData(state.TempData, "item_id")
			var selectedItem models.Item
			for _, item := range b.items {
				if item.ID == itemID {
					selectedItem = item
					break
				}
			}
			// Сохраняем выбранный аппарат для создания заявки
			tempData := map[string]interface{}{
				"item_id": selectedItem.ID,
			}
			b.setUserState(ctx, callback.From.ID, StateWaitingDate, tempData)

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
		b.logger.Warn().Str("callback_data", callback.Data).Msg("Unknown callback data")
	}

	// Обработка выбора аппарата менеджером
	if strings.HasPrefix(data, "manager_select_item:") {
		b.handleManagerItemSelection(ctx, update)
	} else if strings.HasPrefix(data, "manager_items_page:") {
		pageStr := strings.TrimPrefix(data, "manager_items_page:")
		page, err := strconv.Atoi(pageStr)
		if err != nil {
			b.logger.Error().Err(err).Str("page_str", pageStr).Msg("Error parsing page")
			return
		}
		b.editManagerItemsPage(ctx, update, page)
	} else if data == "manager_single_date" {
		b.handleManagerDateType(ctx, update, "single")
	} else if data == "manager_date_range" {
		b.handleManagerDateType(ctx, update, "range")
	}

	// Ответ на callback (убирает "часики" на кнопке)
	b.bot.Send(tgbotapi.NewCallback(callback.ID, ""))
}

// handleScheduleItemSelection обработка выбора аппарата для расписания
func (b *Bot) handleScheduleItemSelection(ctx context.Context, update tgbotapi.Update) {
	callback := update.CallbackQuery
	data := callback.Data

	itemIDStr := strings.TrimPrefix(data, "schedule_select_item:")
	itemID, err := strconv.ParseInt(itemIDStr, 10, 64)
	if err != nil {
		b.logger.Error().Err(err).Str("item_id_str", itemIDStr).Msg("Error parsing item ID")
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
		"item_id": selectedItem.ID,
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
	if state == nil || state.TempData["item_id"] == nil {
		b.sendMessage(chatID, "Ошибка: аппарат не выбран")
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
func (b *Bot) editScheduleItemsPage(ctx context.Context, update tgbotapi.Update, page int) {
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
func (b *Bot) handleItemSelectionFromCallback(ctx context.Context, update tgbotapi.Update) {
	callback := update.CallbackQuery
	data := callback.Data

	itemIDStr := strings.TrimPrefix(data, "select_item:")
	itemID, err := strconv.ParseInt(itemIDStr, 10, 64)
	if err != nil {
		b.logger.Error().Err(err).Str("item_id_str", itemIDStr).Msg("Error parsing item ID")
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
	state.TempData["item_id"] = selectedItem.ID
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
func (b *Bot) editItemsPage(ctx context.Context, update tgbotapi.Update, page int) {
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
func (b *Bot) saveUser(ctx context.Context, update tgbotapi.Update) {
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
	err := b.db.CreateOrUpdateUser(ctx, dbUser)
	if err != nil {
		b.logger.Error().Err(err).Int64("user_id", user.ID).Msg("Ошибка при сохранении пользователя")
	} else {
		b.logger.Info().Str("first_name", user.FirstName).Int64("user_id", user.ID).Msg("Пользователь сохранен")
	}

	b.SyncUsersToSheets(ctx)
}

// updateUserPhone обновляет номер телефона пользователя
func (b *Bot) updateUserPhone(ctx context.Context, telegramID int64, phone string) {
	err := b.db.UpdateUserPhone(ctx, telegramID, phone)
	if err != nil {
		b.logger.Error().Err(err).Int64("telegram_id", telegramID).Msg("Ошибка при обновлении телефона пользователя")
	} else {
		b.logger.Info().Int64("telegram_id", telegramID).Msg("Телефон обновлен для пользователя")
	}
}

// updateUserActivity обновляет время последней активности пользователя
func (b *Bot) updateUserActivity(ctx context.Context, telegramID int64) {
	err := b.db.UpdateUserActivity(ctx, telegramID)
	if err != nil {
		b.logger.Error().Err(err).Int64("user_id", telegramID).Msg("Ошибка при обновлении активности пользователя")
	}
}

// handleStartWithUserTracking обработка /start с сохранением пользователя
func (b *Bot) handleStartWithUserTracking(ctx context.Context, update tgbotapi.Update) {
	// Сохраняем пользователя
	b.saveUser(ctx, update)

	// Обновляем активность
	b.updateUserActivity(ctx, update.Message.From.ID)

	// Показываем главное меню
	b.handleMainMenu(ctx, update)
}

// getUserStats возвращает статистику пользователей (для менеджеров)
func (b *Bot) getUserStats(ctx context.Context, update tgbotapi.Update) {
	if !b.isManager(update.Message.From.ID) {
		return
	}

	// Получаем общую статистику
	allUsers, err := b.db.GetAllUsers(ctx)
	if err != nil {
		b.logger.Error().Err(err).Msg("Error getting users")
		b.sendMessage(update.Message.Chat.ID, "Ошибка при получении статистики")
		return
	}

	activeUsers, err := b.db.GetActiveUsers(ctx, 30) // Активные за последние 30 дней
	if err != nil {
		b.logger.Error().Err(err).Msg("Error getting active users")
	}

	managers, err := b.db.GetUsersByManagerStatus(ctx, true)
	if err != nil {
		b.logger.Error().Err(err).Msg("Error getting managers")
	}

	var blacklistedCount int
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

	statusOrder := []string{"pending", "confirmed", "changed", "completed", "cancelled"}
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

// SyncUsersToSheets синхронизирует пользователей с Google Sheets
func (b *Bot) SyncUsersToSheets(ctx context.Context) {
	if b.sheetsService == nil {
		return
	}

	users, err := b.db.GetAllUsers(ctx)
	if err != nil {
		b.logger.Error().Err(err).Msg("Failed to get users for Google Sheets sync")
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

	err = b.sheetsService.UpdateUsersSheet(ctx, googleUsers)
	if err != nil {
		b.logger.Error().Err(err).Msg("Failed to sync users to Google Sheets")
	} else {
		b.logger.Info().Msg("Users successfully synced to Google Sheets")
	}
}

// SyncBookingsToSheets синхронизирует бронирования с Google Sheets
func (b *Bot) SyncBookingsToSheets(ctx context.Context) {
	if b.sheetsService == nil {
		b.logger.Warn().Msg("Google Sheets service not initialized")
		return
	}

	// Получаем бронирования за период: один месяц назад и два месяца вперед
	startDate := time.Now().AddDate(0, -1, 0) // 1 месяц назад
	endDate := time.Now().AddDate(0, 2, 0)    // 2 месяца вперед

	bookings, err := b.db.GetBookingsByDateRange(ctx, startDate, endDate)
	if err != nil {
		b.logger.Error().Err(err).Msg("Failed to get bookings for Google Sheets sync")
		return
	}

	b.logger.Info().Int("count", len(bookings)).Msg("Syncing bookings to Google Sheets")

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
	err = b.sheetsService.ReplaceBookingsSheet(ctx, googleBookings)
	if err != nil {
		b.logger.Error().Err(err).Msg("Failed to sync bookings to Google Sheets")
	} else {
		b.logger.Info().Int("count", len(googleBookings)).Msg("Bookings successfully synced to Google Sheets")
	}

	// Также синхронизируем расписание
	b.SyncScheduleToSheets(ctx)
}

// AppendBookingToSheets добавляет одно бронирование в Google Sheets
func (b *Bot) AppendBookingToSheets(ctx context.Context, booking *models.Booking) {
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

	err := b.sheetsService.AppendBooking(ctx, googleBooking)
	if err != nil {
		b.logger.Error().Err(err).Int64("booking_id", booking.ID).Msg("Failed to append booking to Google Sheets")
	} else {
		b.logger.Info().Int64("booking_id", booking.ID).Msg("Booking appended to Google Sheets")
	}
}

// appendBookingToSheetsAsync отправляет бронирование в Google Sheets с ретраями, не блокируя основной поток.
func (b *Bot) appendBookingToSheetsAsync(ctx context.Context, booking models.Booking) {
	if b.sheetsService == nil {
		return
	}

	go b.retryWithBackoff(ctx, "append booking to sheets", 3, 2*time.Second, func(c context.Context) error {
		return b.sheetsService.AppendBooking(c, &booking)
	})
}

// syncBookingsToSheetsAsync запускает полную синхронизацию с ретраями в фоне.
func (b *Bot) syncBookingsToSheetsAsync(ctx context.Context) {
	if b.sheetsService == nil {
		return
	}

	go b.retryWithBackoff(ctx, "sync bookings to sheets", 2, 5*time.Second, func(c context.Context) error {
		b.SyncBookingsToSheets(c)
		return nil
	})
}

// retryWithBackoff выполняет fn с экспоненциальной задержкой.
func (b *Bot) retryWithBackoff(ctx context.Context, op string, attempts int, baseDelay time.Duration, fn func(context.Context) error) {
	for i := 0; i < attempts; i++ {
		if err := fn(ctx); err != nil {
			b.logger.Warn().
				Err(err).
				Str("operation", op).
				Int("attempt", i+1).
				Int("max_attempts", attempts).
				Msg("Operation attempt failed")
			
			select {
			case <-ctx.Done():
				return
			case <-time.After(baseDelay * time.Duration(1<<i)):
				continue
			}
		}
		return
	}
	b.logger.Error().Str("operation", op).Int("attempts", attempts).Msg("Operation failed after all attempts")
}

// enqueueBookingUpsert sends an upsert task to the sheets worker if available.
func (b *Bot) enqueueBookingUpsert(ctx context.Context, booking models.Booking) {
	if b.sheetsWorker == nil {
		return
	}
	if err := b.sheetsWorker.EnqueueTask(ctx, worker.SheetTask{
		Type:      worker.TaskUpsert,
		BookingID: booking.ID,
		Booking:   &booking,
	}); err != nil {
		b.logger.Error().Err(err).Int64("booking_id", booking.ID).Msg("sheets enqueue upsert booking error")
	}
}

// enqueueBookingStatus sends a status-only update task to the sheets worker if available.
func (b *Bot) enqueueBookingStatus(ctx context.Context, bookingID int64, status string) {
	if b.sheetsWorker == nil {
		return
	}
	if err := b.sheetsWorker.EnqueueTask(ctx, worker.SheetTask{
		Type:      worker.TaskUpdateStatus,
		BookingID: bookingID,
		Status:    status,
	}); err != nil {
		b.logger.Error().Err(err).Int64("booking_id", bookingID).Msg("sheets enqueue status booking error")
	}
}
