package bot

import (
	"context"
	"strings"
	"time"

	"bronivik/internal/models"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/rs/zerolog"
)

func (b *Bot) handleMessage(ctx context.Context, update *tgbotapi.Update) {
	userID := update.Message.From.ID
	text := update.Message.Text
	l := zerolog.Ctx(ctx)

	if b.metrics != nil {
		b.metrics.MessagesProcessed.Inc()
		if strings.HasPrefix(text, "/") {
			b.metrics.CommandsProcessed.Inc()
		}
	}

	l.Debug().
		Int64("user_id", userID).
		Str("username", update.Message.From.UserName).
		Str("text", text).
		Msg("Handling message")

	if b.isBlacklisted(userID) {
		return
	}

	if b.isManager(userID) && b.handleManagerCommand(ctx, update) {
		return
	}

	state := b.getUserState(ctx, userID)

	// Обработка общих кнопок "Назад" и "Отмена"
	if text == btnCancel || text == btnBack {
		b.handleCustomInput(ctx, update, state)
		return
	}

	if b.handleUserCommands(ctx, update, state) {
		return
	}

	if state != nil && b.handleUserStateSteps(ctx, update, text, state) {
		return
	}

	b.handleCustomInput(ctx, update, state)
}

// handleUserCommands обрабатывает основные команды пользователя
func (b *Bot) handleUserCommands(ctx context.Context, update *tgbotapi.Update, state *models.UserState) bool {
	text := update.Message.Text

	if b.handleBasicCommands(ctx, update, text) {
		return true
	}

	if b.handleNavigationCommands(ctx, update, text) {
		return true
	}

	return b.handleBookingProcessCommands(ctx, update, state, text)
}

func (b *Bot) handleBasicCommands(ctx context.Context, update *tgbotapi.Update, text string) bool {
	switch {
	case text == "/start" || strings.EqualFold(text, "сброс") || strings.EqualFold(text, "reset"):
		b.clearUserState(ctx, update.Message.From.ID)
		b.handleStartWithUserTracking(ctx, update)
		return true

	case text == btnManagerContacts:
		b.showManagerContacts(ctx, update)
		return true

	case text == btnMyBookings:
		b.showUserBookings(ctx, update)
		return true
	}
	return false
}

func (b *Bot) handleNavigationCommands(ctx context.Context, update *tgbotapi.Update, text string) bool {
	switch {
	case text == btnAvailableItems:
		b.showAvailableItems(ctx, update)
		return true

	case text == btnViewSchedule:
		b.handleViewSchedule(ctx, update)
		return true

	case text == btnBackToItems:
		b.handleViewSchedule(ctx, update)
		return true
	}
	return false
}

func (b *Bot) handleBookingProcessCommands(ctx context.Context, update *tgbotapi.Update, state *models.UserState, text string) bool {
	switch {
	case text == btnCreateBooking:
		b.handleSelectItem(ctx, update)
		return true

	case text == btnMonthSchedule:
		if state != nil && state.TempData["item_id"] != nil {
			b.showMonthScheduleForItem(ctx, update)
		} else {
			b.sendMessage(update.Message.Chat.ID, "Сначала выберите аппарат для просмотра расписания")
			b.handleViewSchedule(ctx, update)
		}
		return true

	case text == btnPickDate:
		if state != nil && state.TempData["item_id"] != nil {
			b.requestSpecificDate(ctx, update)
		} else {
			b.sendMessage(update.Message.Chat.ID, "Сначала выберите аппарат для просмотра расписания")
			b.handleViewSchedule(ctx, update)
		}
		return true

	case text == btnCreateForItem:
		if state != nil && state.TempData["item_id"] != nil {
			itemID := state.GetInt64("item_id")
			b.handleDateSelection(ctx, update, itemID)
		}
		return true
	}

	return false
}

// handleUserStateSteps обрабатывает ввод пользователя в зависимости от текущего шага
func (b *Bot) handleUserStateSteps(ctx context.Context, update *tgbotapi.Update, text string, state *models.UserState) bool {
	userID := update.Message.From.ID

	switch state.CurrentStep {
	case models.StateEnterName:
		state.TempData["user_name"] = b.sanitizeInput(text)
		b.setUserState(ctx, userID, models.StatePhoneNumber, state.TempData)
		b.handlePhoneRequest(ctx, update)
		return true

	case models.StatePhoneNumber:
		b.handlePhoneReceived(ctx, update, text)
		return true

	case models.StateWaitingSpecificDate:
		b.handleSpecificDateInput(ctx, update, text)
		return true

	case models.StateWaitingDate:
		b.handleDateInput(ctx, update, text, state)
		return true
	}

	return false
}

func (b *Bot) handleStartWithUserTracking(ctx context.Context, update *tgbotapi.Update) {
	user := &models.User{
		TelegramID:   update.Message.From.ID,
		Username:     update.Message.From.UserName,
		FirstName:    update.Message.From.FirstName,
		LastName:     update.Message.From.LastName,
		LanguageCode: update.Message.From.LanguageCode,
		LastActivity: time.Now(),
		CreatedAt:    time.Now(),
	}

	err := b.userService.SaveUser(ctx, user)
	if err != nil {
		b.logger.Error().Err(err).Int64("user_id", user.TelegramID).Msg("Error tracking user")
	}

	b.handleMainMenu(ctx, update)
}

func (b *Bot) updateUserActivity(userID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := b.userService.UpdateUserActivity(ctx, userID)
	if err != nil {
		b.logger.Error().Err(err).Int64("user_id", userID).Msg("Error updating user activity")
	}
}

func (b *Bot) updateUserPhone(userID int64, phone string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := b.userService.UpdateUserPhone(ctx, userID, phone)
	if err != nil {
		b.logger.Error().Err(err).Int64("user_id", userID).Msg("Error updating user phone")
	}
}
