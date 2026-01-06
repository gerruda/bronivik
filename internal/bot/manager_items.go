package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"bronivik/internal/models"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (b *Bot) handleAddItemCommand(ctx context.Context, update *tgbotapi.Update) {
	parts := strings.Fields(update.Message.Text)
	if len(parts) < 3 {
		b.sendMessage(update.Message.Chat.ID, "Использование: /add_item <название> <количество>")
		return
	}

	qty, err := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	if err != nil || qty <= 0 {
		b.sendMessage(update.Message.Chat.ID, "Количество должно быть положительным числом")
		return
	}

	name := b.sanitizeInput(strings.Join(parts[1:len(parts)-1], " "))
	item := &models.Item{Name: name, TotalQuantity: qty}
	if err := b.itemService.CreateItem(ctx, item); err != nil {
		b.sendMessage(update.Message.Chat.ID, fmt.Sprintf("Не удалось создать аппарат: %v", err))
		return
	}

	b.sendMessage(update.Message.Chat.ID,
		fmt.Sprintf("✅ Аппарат '%s' добавлен (кол-во: %d, порядок: %d)",
			item.Name, item.TotalQuantity, item.SortOrder))
}

func (b *Bot) handleEditItemCommand(ctx context.Context, update *tgbotapi.Update) {
	parts := strings.Fields(update.Message.Text)
	if len(parts) < 3 {
		b.sendMessage(update.Message.Chat.ID, "Использование: /edit_item <название> <новое_количество>")
		return
	}

	qty, err := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	if err != nil || qty <= 0 {
		b.sendMessage(update.Message.Chat.ID, "Количество должно быть положительным числом")
		return
	}

	name := b.sanitizeInput(strings.Join(parts[1:len(parts)-1], " "))
	current, err := b.itemService.GetItemByName(ctx, name)
	if err != nil {
		b.sendMessage(update.Message.Chat.ID, fmt.Sprintf("Аппарат '%s' не найден", name))
		return
	}

	current.TotalQuantity = qty
	if err := b.itemService.UpdateItem(ctx, current); err != nil {
		b.sendMessage(update.Message.Chat.ID, fmt.Sprintf("Не удалось обновить аппарат: %v", err))
		return
	}

	b.sendMessage(update.Message.Chat.ID, fmt.Sprintf("✅ Аппарат '%s' обновлён (кол-во: %d)", current.Name, current.TotalQuantity))
}

func (b *Bot) handleListItemsCommand(ctx context.Context, update *tgbotapi.Update) {
	items, err := b.itemService.GetActiveItems(ctx)
	if err != nil {
		b.sendMessage(update.Message.Chat.ID, fmt.Sprintf("Ошибка загрузки списка: %v", err))
		return
	}

	if len(items) == 0 {
		b.sendMessage(update.Message.Chat.ID, "Активные аппараты отсутствуют")
		return
	}

	var sb strings.Builder
	sb.WriteString("📋 Список активных аппаратов:\n")
	for _, it := range items {
		sb.WriteString(fmt.Sprintf("• %s — qty: %d, order: %d\n", it.Name, it.TotalQuantity, it.SortOrder))
	}

	b.sendMessage(update.Message.Chat.ID, sb.String())
}

func (b *Bot) handleDisableItemCommand(ctx context.Context, update *tgbotapi.Update) {
	parts := strings.Fields(update.Message.Text)
	if len(parts) < 2 {
		b.sendMessage(update.Message.Chat.ID, "Использование: /disable_item <название>")
		return
	}

	name := b.sanitizeInput(strings.Join(parts[1:], " "))
	item, err := b.itemService.GetItemByName(ctx, name)
	if err != nil {
		b.sendMessage(update.Message.Chat.ID, fmt.Sprintf("Аппарат '%s' не найден", name))
		return
	}

	if err := b.itemService.DeactivateItem(ctx, item.ID); err != nil {
		b.sendMessage(update.Message.Chat.ID, fmt.Sprintf("Не удалось отключить аппарат: %v", err))
		return
	}

	b.sendMessage(update.Message.Chat.ID, fmt.Sprintf("🛑 Аппарат '%s' деактивирован", item.Name))
}

func (b *Bot) handleSetItemOrderCommand(ctx context.Context, update *tgbotapi.Update) {
	parts := strings.Fields(update.Message.Text)
	if len(parts) < 3 {
		b.sendMessage(update.Message.Chat.ID, "Использование: /set_item_order <название> <порядок>")
		return
	}

	order, err := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	if err != nil || order < 1 {
		b.sendMessage(update.Message.Chat.ID, "Порядок должен быть положительным числом")
		return
	}

	name := b.sanitizeInput(strings.Join(parts[1:len(parts)-1], " "))
	item, err := b.itemService.GetItemByName(ctx, name)
	if err != nil {
		b.sendMessage(update.Message.Chat.ID, fmt.Sprintf("Аппарат '%s' не найден", name))
		return
	}

	if err := b.itemService.ReorderItem(ctx, item.ID, order); err != nil {
		b.sendMessage(update.Message.Chat.ID, fmt.Sprintf("Не удалось изменить порядок: %v", err))
		return
	}

	b.sendMessage(update.Message.Chat.ID, fmt.Sprintf("↕️ Порядок '%s' установлен на %d", item.Name, order))
}

func (b *Bot) handleMoveItemCommand(ctx context.Context, update *tgbotapi.Update, delta int64) {
	parts := strings.Fields(update.Message.Text)
	if len(parts) < 2 {
		b.sendMessage(update.Message.Chat.ID, "Использование: /move_item_up|/move_item_down <название>")
		return
	}

	name := strings.Join(parts[1:], " ")
	item, err := b.itemService.GetItemByName(ctx, name)
	if err != nil {
		b.sendMessage(update.Message.Chat.ID, fmt.Sprintf("Аппарат '%s' не найден", name))
		return
	}

	newOrder := item.SortOrder + delta
	if newOrder < 1 {
		newOrder = 1
	}

	if err := b.itemService.ReorderItem(ctx, item.ID, newOrder); err != nil {
		b.sendMessage(update.Message.Chat.ID, fmt.Sprintf("Не удалось изменить порядок: %v", err))
		return
	}

	direction := "вверх"
	if delta > 0 {
		direction = "вниз"
	}
	b.sendMessage(update.Message.Chat.ID, fmt.Sprintf("↕️ Аппарат '%s' перемещён %s (новый порядок: %d)", item.Name, direction, newOrder))
}

// editManagerItemsPage редактирует страницу с аппаратами для менеджера
func (b *Bot) editManagerItemsPage(update *tgbotapi.Update, page int) {
	callback := update.CallbackQuery
	b.sendManagerItemsPage(context.Background(), callback.Message.Chat.ID, callback.Message.MessageID, page)
	if _, err := b.tgService.Send(tgbotapi.NewCallback(callback.ID, "")); err != nil {
		b.logger.Error().Err(err).Msg("Failed to send callback in editManagerItemsPage")
	}
}
