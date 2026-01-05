package bot

import (
	"fmt"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// TimeSlot describes a selectable time slot for the calendar.
type TimeSlot struct {
	Label        string
	CallbackData string
	Available    bool
}

// GenerateCalendarKeyboard builds an inline keyboard for a given month.
// availableDates keys are YYYY-MM-DD strings.
func GenerateCalendarKeyboard(year, month int, availableDates map[string]bool) tgbotapi.InlineKeyboardMarkup {
	firstDay := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	weekdayOffset := int(firstDay.Weekday())
	if weekdayOffset == 0 {
		weekdayOffset = 7 // make Monday-first grid
	}
	daysInMonth := daysIn(time.Month(month), year)

	rows := make([][]tgbotapi.InlineKeyboardButton, 0)
	// Weekday header
	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("Пн", "noop"),
		tgbotapi.NewInlineKeyboardButtonData("Вт", "noop"),
		tgbotapi.NewInlineKeyboardButtonData("Ср", "noop"),
		tgbotapi.NewInlineKeyboardButtonData("Чт", "noop"),
		tgbotapi.NewInlineKeyboardButtonData("Пт", "noop"),
		tgbotapi.NewInlineKeyboardButtonData("Сб", "noop"),
		tgbotapi.NewInlineKeyboardButtonData("Вс", "noop"),
	})

	day := 1
	for day <= daysInMonth {
		row := make([]tgbotapi.InlineKeyboardButton, 0, 7)
		for col := 1; col <= 7; col++ {
			if len(rows) == 1 && col < weekdayOffset {
				row = append(row, tgbotapi.NewInlineKeyboardButtonData(" ", "noop"))
				continue
			}
			if day > daysInMonth {
				row = append(row, tgbotapi.NewInlineKeyboardButtonData(" ", "noop"))
				continue
			}
			dateStr := fmt.Sprintf("%04d-%02d-%02d", year, month, day)
			label := fmt.Sprintf("%d", day)
			available := availableDates == nil || availableDates[dateStr]
			if !available {
				label = "·"
			}
			row = append(row, tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("date:%s", dateStr)))
			day++
		}
		rows = append(rows, row)
	}

	return tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// GenerateTimeSlotsKeyboard builds an inline keyboard for time slots of a day.
func GenerateTimeSlotsKeyboard(slots []TimeSlot, selectedDate string) tgbotapi.InlineKeyboardMarkup {
	rows := make([][]tgbotapi.InlineKeyboardButton, 0)
	for _, slot := range slots {
		text := slot.Label
		if !slot.Available {
			text = "⛔ " + slot.Label
		}
		data := slot.CallbackData
		if data == "" {
			data = fmt.Sprintf("slot:%s", slot.Label)
		}
		rows = append(rows, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData(text, data),
		})
	}
	// Add back button
	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", fmt.Sprintf("back:%s", selectedDate)),
	})
	return tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func daysIn(m time.Month, year int) int {
	switch m {
	case time.February:
		if (year%4 == 0 && year%100 != 0) || year%400 == 0 {
			return 29
		}
		return 28
	case time.April, time.June, time.September, time.November:
		return 30
	default:
		return 31
	}
}
