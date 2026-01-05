package bot

import (
"context"
"io"
"testing"
"time"

"bronivik/internal/config"
"bronivik/internal/domain"
"bronivik/internal/models"
tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
"github.com/rs/zerolog"
)

type mockTelegramService struct {
domain.TelegramService
updatesChan  chan tgbotapi.Update
sentMessages []tgbotapi.Chattable
}

func (m *mockTelegramService) GetUpdatesChan(config tgbotapi.UpdateConfig) tgbotapi.UpdatesChannel {
return m.updatesChan
}

func (m *mockTelegramService) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
m.sentMessages = append(m.sentMessages, c)
return tgbotapi.Message{}, nil
}

func (m *mockTelegramService) GetSelf() tgbotapi.User {
return tgbotapi.User{UserName: "test_bot"}
}

type mockRepository struct {
domain.Repository
users map[int64]*models.User
}

func (m *mockRepository) CreateOrUpdateUser(ctx context.Context, user *models.User) error {
if m.users == nil {
m.users = make(map[int64]*models.User)
}
m.users[user.TelegramID] = user
return nil
}

func (m *mockRepository) UpdateUserActivity(ctx context.Context, telegramID int64) error {
return nil
}

type mockStateManager struct {
domain.StateManager
states map[int64]*models.UserState
}

func (m *mockStateManager) SetUserState(ctx context.Context, userID int64, step string, data map[string]interface{}) error {
if m.states == nil {
m.states = make(map[int64]*models.UserState)
}
m.states[userID] = &models.UserState{UserID: userID, CurrentStep: step, TempData: data}
return nil
}

func (m *mockStateManager) GetUserState(ctx context.Context, userID int64) (*models.UserState, error) {
return m.states[userID], nil
}

func (m *mockStateManager) ClearUserState(ctx context.Context, userID int64) error {
delete(m.states, userID)
return nil
}

func (m *mockStateManager) CheckRateLimit(ctx context.Context, userID int64, limit int, window time.Duration) (bool, error) {
return true, nil
}

func TestBotStart(t *testing.T) {
tg := &mockTelegramService{updatesChan: make(chan tgbotapi.Update, 1)}
repo := &mockRepository{users: make(map[int64]*models.User)}
state := &mockStateManager{states: make(map[int64]*models.UserState)}
logger := zerolog.New(io.Discard)

cfg := &config.Config{
		Telegram: config.TelegramConfig{
			BotToken: "test",
		},
	}

b, _ := NewBot(tg, cfg, nil, repo, state, nil, nil, nil, nil, &logger)

ctx, cancel := context.WithCancel(context.Background())

go b.Start(ctx)

// Send /start message
tg.updatesChan <- tgbotapi.Update{
Message: &tgbotapi.Message{
From: &tgbotapi.User{ID: 123, UserName: "testuser"},
Chat: &tgbotapi.Chat{ID: 123},
Text: "/start",
},
}

// Give it a moment to process
time.Sleep(100 * time.Millisecond)
cancel()

if len(repo.users) != 1 {
t.Errorf("expected 1 user in repo, got %d", len(repo.users))
}

if repo.users[123].Username != "testuser" {
t.Errorf("expected username testuser, got %s", repo.users[123].Username)
}

if len(tg.sentMessages) == 0 {
t.Errorf("expected at least one message sent")
}
}

func TestHandleSelectItem(t *testing.T) {
tg := &mockTelegramService{updatesChan: make(chan tgbotapi.Update, 1)}
repo := &mockRepository{}
state := &mockStateManager{states: make(map[int64]*models.UserState)}
logger := zerolog.New(io.Discard)

items := []models.Item{
{ID: 1, Name: "Item 1"},
{ID: 2, Name: "Item 2"},
}

cfg := &config.Config{
Telegram: config.TelegramConfig{BotToken: "test"},
}

b, _ := NewBot(tg, cfg, items, repo, state, nil, nil, nil, nil, &logger)

update := tgbotapi.Update{
Message: &tgbotapi.Message{
From: &tgbotapi.User{ID: 123},
Chat: &tgbotapi.Chat{ID: 123},
Text: "📋 СОЗДАТЬ ЗАЯВКУ",
},
}

b.handleMessage(context.Background(), update)

if state.states[123].CurrentStep != StateSelectItem {
t.Errorf("expected state %s, got %s", StateSelectItem, state.states[123].CurrentStep)
}

if len(tg.sentMessages) == 0 {
t.Errorf("expected message sent")
}
}

func (m *mockTelegramService) Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error) {
return &tgbotapi.APIResponse{Ok: true}, nil
}

func TestHandleCallbackQuery(t *testing.T) {
tg := &mockTelegramService{updatesChan: make(chan tgbotapi.Update, 1)}
repo := &mockRepository{}
state := &mockStateManager{states: make(map[int64]*models.UserState)}
logger := zerolog.New(io.Discard)

items := []models.Item{
{ID: 1, Name: "Item 1"},
}

cfg := &config.Config{
Telegram: config.TelegramConfig{BotToken: "test"},
}

b, _ := NewBot(tg, cfg, items, repo, state, nil, nil, nil, nil, &logger)

update := tgbotapi.Update{
CallbackQuery: &tgbotapi.CallbackQuery{
ID: "cb123",
From: &tgbotapi.User{ID: 123},
Message: &tgbotapi.Message{
Chat: &tgbotapi.Chat{ID: 123},
MessageID: 456,
},
Data: "select_item:1",
},
}

b.handleCallbackQuery(context.Background(), update)

// After selecting an item, it should ask for a date
if state.states[123].CurrentStep != StateWaitingDate {
t.Errorf("expected state %s, got %s", StateWaitingDate, state.states[123].CurrentStep)
}
}

type mockBookingService struct {
domain.BookingService
available bool
}

func (m *mockBookingService) CheckAvailability(ctx context.Context, itemID int64, date time.Time) (bool, error) {
return m.available, nil
}

func TestHandleDateInput(t *testing.T) {
tg := &mockTelegramService{updatesChan: make(chan tgbotapi.Update, 1)}
repo := &mockRepository{}
state := &mockStateManager{states: make(map[int64]*models.UserState)}
logger := zerolog.New(io.Discard)

items := []models.Item{
{ID: 1, Name: "Item 1"},
}

cfg := &config.Config{
Telegram: config.TelegramConfig{BotToken: "test"},
Bot: config.BotConfig{MaxBookingDays: 365},
}

bookingSvc := &mockBookingService{available: true}

b, _ := NewBot(tg, cfg, items, repo, state, nil, nil, nil, bookingSvc, &logger)

state.states[123] = &models.UserState{
UserID: 123,
CurrentStep: StateWaitingDate,
TempData: map[string]interface{}{"item_id": int64(1)},
}

futureDate := time.Now().AddDate(0, 0, 5).Format("02.01.2006")
update := tgbotapi.Update{
Message: &tgbotapi.Message{
From: &tgbotapi.User{ID: 123},
Chat: &tgbotapi.Chat{ID: 123},
Text: futureDate,
},
}

b.handleMessage(context.Background(), update)

if state.states[123].CurrentStep != StateEnterName {
t.Errorf("expected state %s, got %s", StateEnterName, state.states[123].CurrentStep)
}
}

func TestHandleStartWithUserTracking(t *testing.T) {
tg := &mockTelegramService{updatesChan: make(chan tgbotapi.Update, 1)}
repo := &mockRepository{users: make(map[int64]*models.User)}
state := &mockStateManager{states: make(map[int64]*models.UserState)}
logger := zerolog.New(io.Discard)

cfg := &config.Config{
Telegram: config.TelegramConfig{BotToken: "test"},
}

b, _ := NewBot(tg, cfg, nil, repo, state, nil, nil, nil, nil, &logger)

update := tgbotapi.Update{
Message: &tgbotapi.Message{
From: &tgbotapi.User{
ID: 123,
UserName: "testuser",
FirstName: "Test",
LastName: "User",
LanguageCode: "en",
},
Chat: &tgbotapi.Chat{ID: 123},
Text: "/start",
},
}

b.handleStartWithUserTracking(context.Background(), update)

user, ok := repo.users[123]
if !ok {
t.Fatal("user not created in repo")
}

if user.Username != "testuser" || user.FirstName != "Test" {
t.Errorf("user data mismatch: %+v", user)
}

if state.states[123].CurrentStep != StateMainMenu {
t.Errorf("expected state %s, got %s", StateMainMenu, state.states[123].CurrentStep)
}
}
