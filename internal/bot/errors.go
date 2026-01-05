package bot

import (
	"errors"

	"bronivik/internal/database"
)

func (b *Bot) getErrorMessage(err error) string {
	if err == nil {
		return ""
	}

	if errors.Is(err, database.ErrNotAvailable) {
		return "⚠️ Извините, этот аппарат уже забронирован на выбранную дату. Пожалуйста, выберите другое время или аппарат."
	}

	if errors.Is(err, database.ErrPastDate) {
		return "⚠️ Нельзя создавать бронирование на прошедшую дату."
	}

	if errors.Is(err, database.ErrDateTooFar) {
		return "⚠️ Вы не можете бронировать так далеко в будущем. Пожалуйста, выберите более раннюю дату."
	}

	if errors.Is(err, database.ErrConcurrentModification) {
		return "⚠️ Произошла ошибка при сохранении (конфликт версий). Пожалуйста, попробуйте еще раз."
	}

	// Default error message
	return "❌ Произошла ошибка при обработке вашего запроса. Пожалуйста, попробуйте позже или обратитесь к менеджеру."
}
