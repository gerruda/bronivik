package bot

import (
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type BotWrapper struct {
	*tgbotapi.BotAPI
}

func (w *BotWrapper) GetSelf() tgbotapi.User {
	return w.Self
}

func (w *BotWrapper) StopReceivingUpdates() {
	w.BotAPI.StopReceivingUpdates()
}

func NewBotWrapper(bot *tgbotapi.BotAPI) *BotWrapper {
	return &BotWrapper{BotAPI: bot}
}
