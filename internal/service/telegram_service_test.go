package service

import (
	"testing"

	"bronivik/internal/models"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockTelegramSender struct {
	mock.Mock
}

func (m *mockTelegramSender) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
	args := m.Called(c)
	return args.Get(0).(tgbotapi.Message), args.Error(1)
}

func (m *mockTelegramSender) Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error) {
	args := m.Called(c)
	return args.Get(0).(*tgbotapi.APIResponse), args.Error(1)
}

func (m *mockTelegramSender) GetUpdatesChan(config tgbotapi.UpdateConfig) tgbotapi.UpdatesChannel {
	args := m.Called(config)
	return args.Get(0).(tgbotapi.UpdatesChannel)
}

func (m *mockTelegramSender) GetSelf() tgbotapi.User {
	args := m.Called()
	return args.Get(0).(tgbotapi.User)
}

func (m *mockTelegramSender) StopReceivingUpdates() {
	m.Called()
}

func TestTelegramService(t *testing.T) {
	mockSender := new(mockTelegramSender)
	svc := NewTelegramService(mockSender)

	t.Run("SendMessage", func(t *testing.T) {
		mockSender.On("Send", mock.MatchedBy(func(c tgbotapi.Chattable) bool {
			msg, ok := c.(tgbotapi.MessageConfig)
			return ok && msg.Text == "hello" && msg.ChatID == 123
		})).Return(tgbotapi.Message{}, nil).Once()

		_, err := svc.SendMessage(123, "hello")
		assert.NoError(t, err)
		mockSender.AssertExpectations(t)
	})

	t.Run("SendMarkdown", func(t *testing.T) {
		mockSender.On("Send", mock.MatchedBy(func(c tgbotapi.Chattable) bool {
			msg, ok := c.(tgbotapi.MessageConfig)
			return ok && msg.ParseMode == models.ParseModeMarkdown
		})).Return(tgbotapi.Message{}, nil).Once()

		_, err := svc.SendMarkdown(123, "*bold*")
		assert.NoError(t, err)
		mockSender.AssertExpectations(t)
	})

	t.Run("AnswerCallback", func(t *testing.T) {
		mockSender.On("Request", mock.MatchedBy(func(c tgbotapi.Chattable) bool {
			_, ok := c.(tgbotapi.CallbackConfig)
			return ok
		})).Return(&tgbotapi.APIResponse{Ok: true}, nil).Once()

		err := svc.AnswerCallback("cb123", "ok")
		assert.NoError(t, err)
		mockSender.AssertExpectations(t)
	})
}
