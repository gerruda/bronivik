package service

import (
	"bronivik/internal/domain"
	"bronivik/internal/models"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type TelegramService struct {
	bot domain.TelegramSender
}

func NewTelegramService(bot domain.TelegramSender) *TelegramService {
	return &TelegramService{
		bot: bot,
	}
}

func (s *TelegramService) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
	return s.bot.Send(c)
}

func (s *TelegramService) Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error) {
	return s.bot.Request(c)
}

func (s *TelegramService) SendMessage(chatID int64, text string) (tgbotapi.Message, error) {
	msg := tgbotapi.NewMessage(chatID, text)
	return s.bot.Send(msg)
}

func (s *TelegramService) SendMarkdown(chatID int64, text string) (tgbotapi.Message, error) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = models.ParseModeMarkdown
	return s.bot.Send(msg)
}

func (s *TelegramService) SendWithKeyboard(chatID int64, text string, keyboard tgbotapi.ReplyKeyboardMarkup) (tgbotapi.Message, error) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = keyboard
	return s.bot.Send(msg)
}

func (s *TelegramService) SendWithInlineKeyboard(
	chatID int64,
	text string,
	keyboard tgbotapi.InlineKeyboardMarkup,
) (tgbotapi.Message, error) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = keyboard
	return s.bot.Send(msg)
}

func (s *TelegramService) EditMessage(
	chatID int64,
	messageID int,
	text string,
	keyboard *tgbotapi.InlineKeyboardMarkup,
) (tgbotapi.Message, error) {
	if keyboard != nil {
		msg := tgbotapi.NewEditMessageTextAndMarkup(chatID, messageID, text, *keyboard)
		msg.ParseMode = models.ParseModeMarkdown
		return s.bot.Send(msg)
	}
	msg := tgbotapi.NewEditMessageText(chatID, messageID, text)
	msg.ParseMode = models.ParseModeMarkdown
	return s.bot.Send(msg)
}

func (s *TelegramService) AnswerCallback(callbackID, text string) error {
	callback := tgbotapi.NewCallback(callbackID, text)
	_, err := s.bot.Request(callback)
	return err
}

func (s *TelegramService) GetUpdatesChan(config tgbotapi.UpdateConfig) tgbotapi.UpdatesChannel {
	return s.bot.GetUpdatesChan(config)
}

func (s *TelegramService) GetSelf() tgbotapi.User {
	return s.bot.GetSelf()
}

func (s *TelegramService) StopReceivingUpdates() {
	s.bot.StopReceivingUpdates()
}
