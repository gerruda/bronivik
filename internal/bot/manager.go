package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// handleManagerCommand обработка команд менеджера
func (b *Bot) handleManagerCommand(ctx context.Context, update tgbotapi.Update) bool {
	if !b.isManager(update.Message.From.ID) {
		return false
	}

	userID := update.Message.From.ID
	text := update.Message.Text
	state := b.getUserState(ctx, userID)

	switch {
	case text == "👨‍💼 Все заявки":
		b.showManagerBookings(ctx, update)

	case text == "/get_all":
		b.showManagerBookings(ctx, update)

	case text == "➕ Создать заявку (Менеджер)":
		b.startManagerBooking(ctx, update)

	// секретная команда, доступная менеджерам, но не отображаемся у них в меню
	case text == "/stats" && b.isManager(userID):
		b.getUserStats(ctx, update)

	case strings.HasPrefix(text, "/manager_booking_"):
		// Просмотр конкретной заявки
		parts := strings.Split(text, "_")
		if len(parts) >= 3 {
			bookingID, err := strconv.ParseInt(parts[2], 10, 64)
			if err == nil {
				b.showManagerBookingDetail(ctx, update, bookingID)
			}
		}

	case state != nil && state.CurrentStep == "manager_waiting_client_name":
		b.handleManagerClientName(ctx, update, text, state)

	case state != nil && state.CurrentStep == "manager_waiting_client_phone":
		b.handleManagerClientPhone(ctx, update, text, state)

	case state != nil && state.CurrentStep == "manager_waiting_single_date":
		b.handleManagerSingleDate(ctx, update, text, state)

	case state != nil && state.CurrentStep == "manager_waiting_start_date":
		b.handleManagerStartDate(ctx, update, text, state)

	case state != nil && state.CurrentStep == "manager_waiting_end_date":
		b.handleManagerEndDate(ctx, update, text, state)

	case state != nil && state.CurrentStep == "manager_waiting_comment":
		b.handleManagerComment(ctx, update, text, state)

	case state != nil && state.CurrentStep == "manager_confirm_booking" && text == "✅ Подтвердить создание":
		b.createManagerBookings(ctx, update, state)

	case state != nil && state.CurrentStep == "manager_confirm_booking" && text == "❌ Отмена":
		b.clearUserState(ctx, update.Message.From.ID)
		b.sendMessage(update.Message.Chat.ID, "❌ Создание заявки отменено")
		b.handleMainMenu(ctx, update)

	case text == "🔄 Синхронизировать бронирования (Google Sheets)":
		b.sendMessage(update.Message.Chat.ID, "⏳ Запускаю фоновую синхронизацию бронирований...")
		go b.SyncBookingsToSheets(ctx)

	case text == "📅 Синхронизировать расписание (Google Sheets)":
		b.sendMessage(update.Message.Chat.ID, "⏳ Запускаю фоновую синхронизацию расписания...")
		go b.SyncScheduleToSheets(ctx)

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

// handleManagerCallback обработка действий менеджера с заявками
func (b *Bot) handleManagerCallback(ctx context.Context, update tgbotapi.Update) bool {
	callback := update.CallbackQuery
	if callback == nil {
		return false
	}

	data := callback.Data

	// Проверяем пагинацию аппаратов
	if strings.HasPrefix(data, "manager_items_page:") {
		page, _ := strconv.Atoi(strings.TrimPrefix(data, "manager_items_page:"))
		b.editManagerItemsPage(update, page)
		return true
	}

	// Проверяем пагинацию заявок
	if strings.HasPrefix(data, "manager_bookings_page:") {
		page, _ := strconv.Atoi(strings.TrimPrefix(data, "manager_bookings_page:"))
		b.sendManagerBookingsPage(ctx, callback.Message.Chat.ID, callback.Message.MessageID, page)
		return true
	}

	// Проверяем просмотр конкретной заявки из списка
	if strings.HasPrefix(data, "show_booking:") {
		id, _ := strconv.ParseInt(strings.TrimPrefix(data, "show_booking:"), 10, 64)
		booking, err := b.db.GetBooking(ctx, id)
		if err == nil {
			b.sendManagerBookingDetail(ctx, callback.Message.Chat.ID, booking)
		}
		return true
	}

	// Проверяем выбор аппарата
	if strings.HasPrefix(data, "manager_select_item:") {
		b.handleManagerItemSelection(ctx, update)
		return true
	}

	// Проверяем тип даты
	if data == "manager_single_date" {
		b.handleManagerDateType(ctx, update, "single")
		return true
	}
	if data == "manager_date_range" {
		b.handleManagerDateType(ctx, update, "range")
		return true
	}

	// Проверяем изменение аппарата
	if strings.HasPrefix(data, "change_to_") {
		b.handleChangeItem(ctx, update)
		return true
	}

	// Проверяем кнопку "Позвонить"
	if strings.HasPrefix(data, "call_booking:") {
		b.handleCallButton(ctx, update)
		return true
	}

	// Проверяем экспорт пользователей
	if data == "export_users" {
		b.handleExportUsers(ctx, update)
		return true
	}

	// Обработка действий с заявками (confirm, reject, etc.)
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

	booking, err := b.db.GetBooking(ctx, bookingID)
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

	// Обновляем сообщение у менеджера (если это не изменение аппарата, которое само обновляет)
	if action != "change_item_" {
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID,
			fmt.Sprintf("✅ Заявка #%d обработана\nДействие: %s", bookingID, action))
		b.tgService.Send(editMsg)
	}

	return true
}
