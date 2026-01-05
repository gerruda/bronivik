package bot

import (
	"context"
	"strings"
	"time"

	"bronivik/internal/models"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/rs/zerolog"
)

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
			b.handleDateSelection(ctx, update, itemID)
		}

	case state != nil && state.CurrentStep == StateEnterName:
		state.TempData["user_name"] = text
		b.setUserState(ctx, userID, StatePhoneNumber, state.TempData)
		b.handlePhoneRequest(ctx, update)

	case state != nil && state.CurrentStep == StatePhoneNumber:
		b.handlePhoneReceived(ctx, update, text)

	case state != nil && state.CurrentStep == StateWaitingSpecificDate:
		b.handleSpecificDateInput(ctx, update, text)

	case state != nil && state.CurrentStep == StateWaitingDate:
		b.handleDateInput(ctx, update, text, state)

	default:
		b.handleCustomInput(ctx, update, state)
	}
}

func (b *Bot) handleStartWithUserTracking(ctx context.Context, update tgbotapi.Update) {
	user := &models.User{
		TelegramID:   update.Message.From.ID,
		Username:     update.Message.From.UserName,
		FirstName:    update.Message.From.FirstName,
		LastName:     update.Message.From.LastName,
		LanguageCode: update.Message.From.LanguageCode,
		LastActivity: time.Now(),
		CreatedAt:    time.Now(),
	}

	err := b.db.CreateOrUpdateUser(ctx, user)
	if err != nil {
		b.logger.Error().Err(err).Int64("user_id", user.TelegramID).Msg("Error tracking user")
	}

	b.handleMainMenu(ctx, update)
}

func (b *Bot) updateUserActivity(userID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := b.db.UpdateUserActivity(ctx, userID)
	if err != nil {
		b.logger.Error().Err(err).Int64("user_id", userID).Msg("Error updating user activity")
	}
}

func (b *Bot) updateUserPhone(userID int64, phone string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := b.db.UpdateUserPhone(ctx, userID, phone)
	if err != nil {
		b.logger.Error().Err(err).Int64("user_id", userID).Msg("Error updating user phone")
	}
}
