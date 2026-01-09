package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"bronivik/internal/models"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// handleManagerCommand обработка команд менеджера
func (b *Bot) handleManagerCommand(ctx context.Context, update *tgbotapi.Update) bool {
	if !b.isManager(update.Message.From.ID) {
		return false
	}

	userID := update.Message.From.ID
	text := update.Message.Text
	state := b.getUserState(ctx, userID)

	// Команды без учета состояния
	if b.handleManagerBasicCommands(ctx, update, text, userID) {
		return true
	}

	// Команды управления аппаратами
	if b.handleManagerItemCommands(ctx, update, text) {
		return true
	}

	// Команды с учетом состояния
	if state != nil && b.handleManagerStateCommands(ctx, update, text, state) {
		return true
	}

	return false
}

// handleManagerBasicCommands обрабатывает основные команды менеджера
func (b *Bot) handleManagerBasicCommands(ctx context.Context, update *tgbotapi.Update, text string, userID int64) bool {
	switch {
	case text == btnAllBookings || text == "/get_all":
		b.showManagerBookings(ctx, update)
		return true

	case text == btnCreateBookingManager:
		b.startManagerBooking(ctx, update)
		return true

	case text == "/stats" && b.isManager(userID):
		b.getUserStats(ctx, update)
		return true

	case strings.HasPrefix(text, "/manager_booking_"):
		parts := strings.Split(text, "_")
		if len(parts) >= 3 {
			if bookingID, err := strconv.ParseInt(parts[2], 10, 64); err == nil {
				b.showManagerBookingDetail(ctx, update, bookingID)
			}
		}
		return true

	case text == btnSyncBookings:
		b.sendMessage(update.Message.Chat.ID, "⏳ Запускаю фоновую синхронизацию бронирований...")
		go b.SyncBookingsToSheets(ctx)
		return true

	case text == btnSyncSchedule:
		b.sendMessage(update.Message.Chat.ID, "⏳ Запускаю фоновую синхронизацию расписания...")
		go b.SyncScheduleToSheets(ctx)
		return true
	}
	return false
}

// handleManagerItemCommands обрабатывает команды управления аппаратами
func (b *Bot) handleManagerItemCommands(ctx context.Context, update *tgbotapi.Update, text string) bool {
	switch {
	case strings.HasPrefix(text, "/add_item"):
		b.handleAddItemCommand(ctx, update)
		return true
	case strings.HasPrefix(text, "/edit_item"):
		b.handleEditItemCommand(ctx, update)
		return true
	case strings.HasPrefix(text, "/list_items"):
		b.handleListItemsCommand(ctx, update)
		return true
	case strings.HasPrefix(text, "/disable_item"):
		b.handleDisableItemCommand(ctx, update)
		return true
	case strings.HasPrefix(text, "/set_item_order"):
		b.handleSetItemOrderCommand(ctx, update)
		return true
	case strings.HasPrefix(text, "/move_item_up"):
		b.handleMoveItemCommand(ctx, update, -1)
		return true
	case strings.HasPrefix(text, "/move_item_down"):
		b.handleMoveItemCommand(ctx, update, 1)
		return true
	}
	return false
}

// handleManagerStateCommands обрабатывает команды менеджера в зависимости от состояния
func (b *Bot) handleManagerStateCommands(ctx context.Context, update *tgbotapi.Update, text string, state *models.UserState) bool {
	switch state.CurrentStep {
	case models.StateManagerWaitingClientName:
		b.handleManagerClientName(ctx, update, text, state)
		return true
	case models.StateManagerWaitingClientPhone:
		b.handleManagerClientPhone(ctx, update, text, state)
		return true
	case models.StateManagerWaitingSingleDate:
		b.handleManagerSingleDate(ctx, update, text, state)
		return true
	case models.StateManagerWaitingStartDate:
		b.handleManagerStartDate(ctx, update, text, state)
		return true
	case models.StateManagerWaitingEndDate:
		b.handleManagerEndDate(ctx, update, text, state)
		return true
	case models.StateManagerWaitingComment:
		b.handleManagerComment(ctx, update, text, state)
		return true
	case models.StateManagerConfirmBooking:
		if text == btnConfirmCreate {
			b.createManagerBookings(ctx, update, state)
			return true
		} else if text == btnCancel {
			b.clearUserState(ctx, update.Message.From.ID)
			b.sendMessage(update.Message.Chat.ID, "❌ Создание заявки отменено")
			b.handleMainMenu(ctx, update)
			return true
		}
	}
	return false
}

// handleManagerCallback обработка действий менеджера с заявками
func (b *Bot) handleManagerCallback(ctx context.Context, update *tgbotapi.Update) bool {
	callback := update.CallbackQuery
	if callback == nil {
		return false
	}

	data := callback.Data

	// Проверяем пагинацию и выбор
	if b.handleManagerPaginationAndSelection(ctx, update, data) {
		return true
	}

	// Проверяем тип даты и другие действия
	if b.handleManagerMiscCallbacks(ctx, update, data) {
		return true
	}

	// Обработка действий с заявками (confirm, reject, etc.)
	return b.handleManagerBookingActions(ctx, update, data)
}

// handleManagerPaginationAndSelection обрабатывает пагинацию и выбор аппаратов/заявок
func (b *Bot) handleManagerPaginationAndSelection(ctx context.Context, update *tgbotapi.Update, data string) bool {
	callback := update.CallbackQuery
	switch {
	case strings.HasPrefix(data, "manager_items_page:"):
		page, _ := strconv.Atoi(strings.TrimPrefix(data, "manager_items_page:"))
		b.editManagerItemsPage(update, page)
		return true

	case strings.HasPrefix(data, "manager_bookings_page:"):
		page, _ := strconv.Atoi(strings.TrimPrefix(data, "manager_bookings_page:"))
		b.sendManagerBookingsPage(ctx, callback.Message.Chat.ID, callback.Message.MessageID, page)
		return true

	case strings.HasPrefix(data, "show_booking:"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(data, "show_booking:"), 10, 64)
		if booking, err := b.bookingService.GetBooking(ctx, id); err == nil {
			b.sendManagerBookingDetail(ctx, callback.Message.Chat.ID, booking)
		}
		return true

	case strings.HasPrefix(data, "manager_select_item:"):
		b.handleManagerItemSelection(ctx, update)
		return true
	}
	return false
}

// handleManagerMiscCallbacks обрабатывает прочие callback-запросы менеджера
func (b *Bot) handleManagerMiscCallbacks(ctx context.Context, update *tgbotapi.Update, data string) bool {
	switch {
	case data == "manager_single_date":
		b.handleManagerDateType(ctx, update, "single")
		return true
	case data == "manager_date_range":
		b.handleManagerDateType(ctx, update, "range")
		return true
	case strings.HasPrefix(data, "change_to_"):
		b.handleChangeItem(ctx, update)
		return true
	case strings.HasPrefix(data, "call_booking:"):
		b.handleCallButton(ctx, update)
		return true
	case data == "export_users":
		b.handleExportUsers(ctx, update)
		return true
	}
	return false
}

// handleManagerBookingActions обрабатывает действия над заявками
func (b *Bot) handleManagerBookingActions(ctx context.Context, update *tgbotapi.Update, data string) bool {
	callback := update.CallbackQuery
	var bookingID int64
	var action string

	actions := []string{"confirm_", "reject_", "reschedule_", "change_item_", "reopen_", "complete_"}
	for _, act := range actions {
		if strings.HasPrefix(data, act) {
			idStr := strings.TrimPrefix(data, act)
			id, err := strconv.ParseInt(idStr, 10, 64)
			if err == nil {
				bookingID = id
				action = act
				break
			}
		}
	}

	if action == "" {
		return false
	}

	booking, err := b.bookingService.GetBooking(ctx, bookingID)
	if err != nil {
		b.logger.Error().Err(err).Int64("booking_id", bookingID).Msg("Error getting booking")
		return true
	}

	switch action {
	case "confirm_":
		b.confirmBooking(ctx, booking, callback.Message.Chat.ID)
	case "reject_":
		b.rejectBooking(ctx, booking, callback.Message.Chat.ID)
	case "reschedule_":
		b.rescheduleBooking(ctx, booking, callback.Message.Chat.ID)
	case "change_item_":
		b.startChangeItem(ctx, booking, callback.Message.Chat.ID)
	case "reopen_":
		b.reopenBooking(ctx, booking, callback.Message.Chat.ID)
	case "complete_":
		b.completeBooking(ctx, booking, callback.Message.Chat.ID)
	}

	if action != "change_item_" {
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID,
			fmt.Sprintf("✅ Заявка #%d обработана\nДействие: %s", bookingID, action))
		if _, err := b.tgService.Send(editMsg); err != nil {
			b.logger.Error().Err(err).Msg("Failed to send edit message in handleManagerBookingActions")
		}
	}

	return true
}
