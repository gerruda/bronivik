package bot

import (
	"context"
	"fmt"
	"strings"

	"bronivik/internal/models"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type PaginationParams struct {
	Ctx          context.Context
	ChatID       int64
	MessageID    int // 0 if new message
	Page         int
	Title        string
	ItemPrefix   string
	PagePrefix   string
	BackCallback string
	ShowCapacity bool
}

// renderPaginatedList - универсальная функция для отрисовки пагинированного списка
func (b *Bot) renderPaginatedList(params PaginationParams, totalCount int, itemsPerPage int, renderer func(startIdx, endIdx int) (string, [][]tgbotapi.InlineKeyboardButton)) {
	if itemsPerPage <= 0 {
		itemsPerPage = b.config.Bot.PaginationSize
	}
	if itemsPerPage <= 0 {
		itemsPerPage = models.DefaultPaginationSize
	}

	startIdx := params.Page * itemsPerPage
	endIdx := startIdx + itemsPerPage
	if endIdx > totalCount {
		endIdx = totalCount
	}

	totalPages := (totalCount + itemsPerPage - 1) / itemsPerPage
	if params.Page >= totalPages && totalPages > 0 {
		params.Page = totalPages - 1
		startIdx = params.Page * itemsPerPage
		endIdx = totalCount
	}

	content, keyboard := renderer(startIdx, endIdx)

	var message strings.Builder
	message.WriteString(fmt.Sprintf("%s\n\n", params.Title))
	if totalPages > 1 {
		message.WriteString(fmt.Sprintf("Страница %d из %d\n\n", params.Page+1, totalPages))
	}
	message.WriteString(content)

	// Добавляем навигационные кнопки
	var navButtons []tgbotapi.InlineKeyboardButton
	if params.Page > 0 {
		navButtons = append(navButtons, tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", fmt.Sprintf("%s%d", params.PagePrefix, params.Page-1)))
	}
	if endIdx < totalCount {
		navButtons = append(navButtons, tgbotapi.NewInlineKeyboardButtonData("Вперед ➡️", fmt.Sprintf("%s%d", params.PagePrefix, params.Page+1)))
	}
	if len(navButtons) > 0 {
		keyboard = append(keyboard, navButtons)
	}

	if params.BackCallback != "" {
		keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад в меню", params.BackCallback),
		})
	}

	markup := tgbotapi.NewInlineKeyboardMarkup(keyboard...)

	if params.MessageID != 0 {
		editMsg := tgbotapi.NewEditMessageTextAndMarkup(
			params.ChatID,
			params.MessageID,
			message.String(),
			markup,
		)
		editMsg.ParseMode = "Markdown"
		b.tgService.Send(editMsg)
	} else {
		msg := tgbotapi.NewMessage(params.ChatID, message.String())
		msg.ReplyMarkup = markup
		msg.ParseMode = "Markdown"
		b.tgService.Send(msg)
	}
}

// renderPaginatedItems - обертка для списка аппаратов
func (b *Bot) renderPaginatedItems(params PaginationParams) {
	items, err := b.itemService.GetActiveItems(params.Ctx)
	if err != nil {
		b.logger.Error().Err(err).Msg("Error getting active items for pagination")
		b.sendMessage(params.ChatID, "Ошибка при получении списка аппаратов")
		return
	}

	b.renderPaginatedList(params, len(items), 8, func(startIdx, endIdx int) (string, [][]tgbotapi.InlineKeyboardButton) {
		var content strings.Builder
		var keyboard [][]tgbotapi.InlineKeyboardButton

		currentItems := items[startIdx:endIdx]
		for i, item := range currentItems {
			content.WriteString(fmt.Sprintf("%d. *%s*\n", startIdx+i+1, item.Name))
			if item.Description != "" {
				content.WriteString(fmt.Sprintf("   📝 %s\n", item.Description))
			}
			if params.ShowCapacity {
				content.WriteString(fmt.Sprintf("   👥 Всего: %d\n", item.TotalQuantity))
			}
			content.WriteString("\n")

			btn := tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("%d. %s", startIdx+i+1, item.Name),
				fmt.Sprintf("%s%d", params.ItemPrefix, item.ID),
			)
			keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{btn})
		}

		return content.String(), keyboard
	})
}

// renderPaginatedBookings - обертка для списка заявок
func (b *Bot) renderPaginatedBookings(params PaginationParams, bookings []models.Booking) {
	b.renderPaginatedList(params, len(bookings), 5, func(startIdx, endIdx int) (string, [][]tgbotapi.InlineKeyboardButton) {
		var content strings.Builder
		var keyboard [][]tgbotapi.InlineKeyboardButton

		currentBookings := bookings[startIdx:endIdx]
		for _, booking := range currentBookings {
			statusEmoji := "⏳"
			switch booking.Status {
			case models.StatusConfirmed:
				statusEmoji = "✅"
			case models.StatusCancelled:
				statusEmoji = "❌"
			case models.StatusChanged:
				statusEmoji = "🔄"
			case models.StatusCompleted:
				statusEmoji = "🏁"
			}

			content.WriteString(fmt.Sprintf("%s *Заявка #%d*\n", statusEmoji, booking.ID))
			content.WriteString(fmt.Sprintf("   👤 %s\n", booking.UserName))
			content.WriteString(fmt.Sprintf("   🏢 %s\n", booking.ItemName))
			content.WriteString(fmt.Sprintf("   📅 %s\n", booking.Date.Format("02.01.2006")))
			content.WriteString(fmt.Sprintf("   🔗 /manager_booking_%d\n\n", booking.ID))

			btn := tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("#%d: %s (%s)", booking.ID, booking.UserName, booking.Date.Format("02.01")),
				fmt.Sprintf("%s%d", params.ItemPrefix, booking.ID),
			)
			keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{btn})
		}

		return content.String(), keyboard
	})
}
