package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"bronivik/internal/models"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (b *Bot) handleCallbackQuery(ctx context.Context, update tgbotapi.Update) {
	callback := update.CallbackQuery
	data := callback.Data
	userID := callback.From.ID

	// Отвечаем на callback сразу, чтобы убрать "часики"
	callbackConfig := tgbotapi.NewCallback(callback.ID, "")
	b.tgService.Request(callbackConfig)

	if b.isBlacklisted(userID) {
		return
	}

	// Обработка команд менеджера
	if b.isManager(userID) {
		if b.handleManagerCallback(ctx, update) {
			return
		}
	}

	// Обработка общих команд
	switch {
	case data == "back_to_main":
		b.clearUserState(ctx, userID)
		b.handleMainMenu(ctx, update)

	case data == "back_to_main_from_schedule":
		b.clearUserState(ctx, userID)
		b.handleMainMenu(ctx, update)

	case strings.HasPrefix(data, "items_page:"):
		page, _ := strconv.Atoi(strings.TrimPrefix(data, "items_page:"))
		b.sendItemsPage(ctx, callback.Message.Chat.ID, callback.Message.MessageID, page)

	case strings.HasPrefix(data, "select_item:"):
		itemID, _ := strconv.ParseInt(strings.TrimPrefix(data, "select_item:"), 10, 64)
		b.handleDateSelection(ctx, update, itemID)

	case strings.HasPrefix(data, "schedule_items_page:"):
		page, _ := strconv.Atoi(strings.TrimPrefix(data, "schedule_items_page:"))
		b.sendScheduleItemsPage(ctx, callback.Message.Chat.ID, callback.Message.MessageID, page)

	case strings.HasPrefix(data, "schedule_select_item:"):
		itemID, _ := strconv.ParseInt(strings.TrimPrefix(data, "schedule_select_item:"), 10, 64)
		b.handleScheduleItemSelected(ctx, update, itemID)

	case data == "start_the_order":
		b.handleSelectItem(ctx, update)

	case data == "start_the_order_item":
		state := b.getUserState(ctx, userID)
		if state != nil && state.TempData["item_id"] != nil {
			itemID := state.GetInt64("item_id")
			b.handleDateSelection(ctx, update, itemID)
		}
	}
}

func (b *Bot) handleDateSelection(ctx context.Context, update tgbotapi.Update, itemID int64) {
	selectedItem, err := b.itemService.GetItemByID(ctx, itemID)
	if err != nil {
		b.sendMessage(update.CallbackQuery.Message.Chat.ID, "Ошибка: аппарат не найден")
		return
	}

	msg := tgbotapi.NewMessage(update.CallbackQuery.Message.Chat.ID,
		fmt.Sprintf("Вы выбрали: %s\n\nВведите дату в формате ДД.ММ.ГГГГ (например, 25.12.2024):", selectedItem.Name))

	b.setUserState(ctx, update.CallbackQuery.From.ID, models.StateWaitingDate, map[string]interface{}{
		"item_id": itemID,
	})

	b.tgService.Send(msg)
}

func (b *Bot) handleScheduleItemSelected(ctx context.Context, update tgbotapi.Update, itemID int64) {
	selectedItem, err := b.itemService.GetItemByID(ctx, itemID)
	if err != nil {
		b.sendMessage(update.CallbackQuery.Message.Chat.ID, "Ошибка: аппарат не найден")
		return
	}

	// Сохраняем выбранный аппарат в состоянии
	b.setUserState(ctx, update.CallbackQuery.From.ID, models.StateViewSchedule, map[string]interface{}{
		"item_id": itemID,
	})

	msg := tgbotapi.NewMessage(update.CallbackQuery.Message.Chat.ID,
		fmt.Sprintf("Выбран аппарат: %s\n\nВыберите период или введите дату:", selectedItem.Name))

	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📅 30 дней"),
			tgbotapi.NewKeyboardButton("🗓 Выбрать дату"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("⬅️ Назад к выбору аппарата"),
		),
	)
	msg.ReplyMarkup = keyboard

	b.tgService.Send(msg)
}
