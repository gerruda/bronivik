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

// Mocks
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

func (m *mockTelegramService) Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error) {
	return &tgbotapi.APIResponse{Ok: true}, nil
}

func (m *mockTelegramService) SendMessage(chatID int64, text string) (tgbotapi.Message, error) {
	m.sentMessages = append(m.sentMessages, tgbotapi.NewMessage(chatID, text))
	return tgbotapi.Message{}, nil
}

func (m *mockTelegramService) SendMarkdown(chatID int64, text string) (tgbotapi.Message, error) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	m.sentMessages = append(m.sentMessages, msg)
	return tgbotapi.Message{}, nil
}

func (m *mockTelegramService) SendWithKeyboard(chatID int64, text string, keyboard tgbotapi.ReplyKeyboardMarkup) (tgbotapi.Message, error) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = keyboard
	m.sentMessages = append(m.sentMessages, msg)
	return tgbotapi.Message{}, nil
}

func (m *mockTelegramService) SendWithInlineKeyboard(chatID int64, text string, keyboard tgbotapi.InlineKeyboardMarkup) (tgbotapi.Message, error) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = keyboard
	m.sentMessages = append(m.sentMessages, msg)
	return tgbotapi.Message{}, nil
}

func (m *mockTelegramService) EditMessage(chatID int64, messageID int, text string, keyboard *tgbotapi.InlineKeyboardMarkup) (tgbotapi.Message, error) {
	return tgbotapi.Message{}, nil
}

func (m *mockTelegramService) AnswerCallback(callbackID string, text string) error {
	return nil
}

func (m *mockTelegramService) StopReceivingUpdates() {}

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

type mockUserService struct {
	domain.UserService
	users map[int64]*models.User
}

func (m *mockUserService) SaveUser(ctx context.Context, user *models.User) error {
	if m.users == nil {
		m.users = make(map[int64]*models.User)
	}
	m.users[user.TelegramID] = user
	return nil
}

func (m *mockUserService) UpdateUserActivity(ctx context.Context, telegramID int64) error {
	return nil
}

func (m *mockUserService) UpdateUserPhone(ctx context.Context, telegramID int64, phone string) error {
	if u, ok := m.users[telegramID]; ok {
		u.Phone = phone
	}
	return nil
}

func (m *mockUserService) IsManager(userID int64) bool {
	return false
}

func (m *mockUserService) IsBlacklisted(userID int64) bool {
	return false
}

func (m *mockUserService) GetUserByID(ctx context.Context, id int64) (*models.User, error) {
	return nil, nil
}

type mockItemService struct {
	domain.ItemService
	items []models.Item
}

func (m *mockItemService) GetActiveItems(ctx context.Context) ([]models.Item, error) {
	return m.items, nil
}

func (m *mockItemService) GetItemByID(ctx context.Context, id int64) (*models.Item, error) {
	for _, item := range m.items {
		if item.ID == id {
			return &item, nil
		}
	}
	return nil, nil
}

type mockBookingService struct {
	domain.BookingService
	available bool
	bookings  map[int64]*models.Booking
}

func (m *mockBookingService) CheckAvailability(ctx context.Context, itemID int64, date time.Time) (bool, error) {
	return m.available, nil
}

func (m *mockBookingService) CreateBooking(ctx context.Context, booking *models.Booking) error {
	booking.ID = int64(len(m.bookings) + 1)
	if m.bookings == nil {
		m.bookings = make(map[int64]*models.Booking)
	}
	m.bookings[booking.ID] = booking
	return nil
}

func (m *mockBookingService) ValidateBookingDate(date time.Time) error {
	return nil
}

func (m *mockBookingService) ConfirmBooking(ctx context.Context, bookingID int64, version int64, managerID int64) error {
	if b, ok := m.bookings[bookingID]; ok {
		b.Status = models.StatusConfirmed
		return nil
	}
	return nil
}

func (m *mockBookingService) CompleteBooking(ctx context.Context, bookingID int64, version int64, managerID int64) error {
	if b, ok := m.bookings[bookingID]; ok {
		b.Status = models.StatusCompleted
		return nil
	}
	return nil
}

func (m *mockBookingService) GetBooking(ctx context.Context, id int64) (*models.Booking, error) {
	return m.bookings[id], nil
}

type mockSheetsWriter struct {
	domain.SheetsWriter
}

func (m *mockSheetsWriter) AppendBooking(ctx context.Context, booking *models.Booking) error {
	return nil
}

type mockSyncWorker struct {
	domain.SyncWorker
}

func (m *mockSyncWorker) EnqueueTask(ctx context.Context, taskType string, bookingID int64, booking *models.Booking, status string) error {
	return nil
}

func (m *mockSyncWorker) EnqueueSyncSchedule(ctx context.Context, startDate, endDate time.Time) error {
	return nil
}

type mockEventPublisher struct {
	domain.EventPublisher
}

func (m *mockEventPublisher) PublishJSON(eventType string, payload interface{}) error {
	return nil
}

// Tests
func TestBotStart(t *testing.T) {
	tg := &mockTelegramService{updatesChan: make(chan tgbotapi.Update, 1)}
	state := &mockStateManager{states: make(map[int64]*models.UserState)}
	userSvc := &mockUserService{users: make(map[int64]*models.User)}
	itemSvc := &mockItemService{}
	bookingSvc := &mockBookingService{}
	sheetsSvc := &mockSheetsWriter{}
	worker := &mockSyncWorker{}
	events := &mockEventPublisher{}
	logger := zerolog.New(io.Discard)

	cfg := &config.Config{
		Telegram: config.TelegramConfig{
			BotToken: "test",
		},
	}

	b, _ := NewBot(tg, cfg, state, sheetsSvc, worker, events, bookingSvc, userSvc, itemSvc, nil, &logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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

	if len(userSvc.users) != 1 {
		t.Errorf("expected 1 user in repo, got %d", len(userSvc.users))
	}

	if userSvc.users[123].Username != "testuser" {
		t.Errorf("expected username testuser, got %s", userSvc.users[123].Username)
	}

	if len(tg.sentMessages) == 0 {
		t.Errorf("expected at least one message sent")
	}
}

func TestHandleSelectItem(t *testing.T) {
	tg := &mockTelegramService{updatesChan: make(chan tgbotapi.Update, 1)}
	state := &mockStateManager{states: make(map[int64]*models.UserState)}
	userSvc := &mockUserService{}
	itemSvc := &mockItemService{
		items: []models.Item{
			{ID: 1, Name: "Item 1"},
			{ID: 2, Name: "Item 2"},
		},
	}
	bookingSvc := &mockBookingService{}
	sheetsSvc := &mockSheetsWriter{}
	worker := &mockSyncWorker{}
	events := &mockEventPublisher{}
	logger := zerolog.New(io.Discard)

	cfg := &config.Config{
		Telegram: config.TelegramConfig{BotToken: "test"},
	}

	b, _ := NewBot(tg, cfg, state, sheetsSvc, worker, events, bookingSvc, userSvc, itemSvc, nil, &logger)

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

func TestHandleCallbackQuery(t *testing.T) {
	tg := &mockTelegramService{updatesChan: make(chan tgbotapi.Update, 1)}
	state := &mockStateManager{states: make(map[int64]*models.UserState)}
	userSvc := &mockUserService{}
	itemSvc := &mockItemService{
		items: []models.Item{
			{ID: 1, Name: "Item 1"},
		},
	}
	bookingSvc := &mockBookingService{}
	sheetsSvc := &mockSheetsWriter{}
	worker := &mockSyncWorker{}
	events := &mockEventPublisher{}
	logger := zerolog.New(io.Discard)

	cfg := &config.Config{
		Telegram: config.TelegramConfig{BotToken: "test"},
	}

	b, _ := NewBot(tg, cfg, state, sheetsSvc, worker, events, bookingSvc, userSvc, itemSvc, nil, &logger)

	update := tgbotapi.Update{
		CallbackQuery: &tgbotapi.CallbackQuery{
			ID:   "cb123",
			From: &tgbotapi.User{ID: 123},
			Message: &tgbotapi.Message{
				Chat:      &tgbotapi.Chat{ID: 123},
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

func TestHandleDateInput(t *testing.T) {
	tg := &mockTelegramService{updatesChan: make(chan tgbotapi.Update, 1)}
	state := &mockStateManager{states: make(map[int64]*models.UserState)}
	userSvc := &mockUserService{}
	itemSvc := &mockItemService{
		items: []models.Item{
			{ID: 1, Name: "Item 1"},
		},
	}
	bookingSvc := &mockBookingService{available: true}
	sheetsSvc := &mockSheetsWriter{}
	worker := &mockSyncWorker{}
	events := &mockEventPublisher{}
	logger := zerolog.New(io.Discard)

	cfg := &config.Config{
		Telegram: config.TelegramConfig{BotToken: "test"},
		Bot:      config.BotConfig{MaxBookingDays: 365},
	}

	b, _ := NewBot(tg, cfg, state, sheetsSvc, worker, events, bookingSvc, userSvc, itemSvc, nil, &logger)

	state.states[123] = &models.UserState{
		UserID:      123,
		CurrentStep: StateWaitingDate,
		TempData:    map[string]interface{}{"item_id": int64(1)},
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
	state := &mockStateManager{states: make(map[int64]*models.UserState)}
	userSvc := &mockUserService{users: make(map[int64]*models.User)}
	itemSvc := &mockItemService{}
	bookingSvc := &mockBookingService{}
	sheetsSvc := &mockSheetsWriter{}
	worker := &mockSyncWorker{}
	events := &mockEventPublisher{}
	logger := zerolog.New(io.Discard)

	cfg := &config.Config{
		Telegram: config.TelegramConfig{BotToken: "test"},
	}

	b, _ := NewBot(tg, cfg, state, sheetsSvc, worker, events, bookingSvc, userSvc, itemSvc, nil, &logger)

	update := tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{
				ID:           123,
				UserName:     "testuser",
				FirstName:    "Test",
				LastName:     "User",
				LanguageCode: "en",
			},
			Chat: &tgbotapi.Chat{ID: 123},
			Text: "/start",
		},
	}

	b.handleStartWithUserTracking(context.Background(), update)

	user, ok := userSvc.users[123]
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

func TestHandlePhoneReceived(t *testing.T) {
	tg := &mockTelegramService{updatesChan: make(chan tgbotapi.Update, 1)}
	state := &mockStateManager{states: make(map[int64]*models.UserState)}
	userSvc := &mockUserService{users: make(map[int64]*models.User)}
	itemSvc := &mockItemService{
		items: []models.Item{
			{ID: 1, Name: "Item 1"},
		},
	}
	bookingSvc := &mockBookingService{available: true}
	sheetsSvc := &mockSheetsWriter{}
	worker := &mockSyncWorker{}
	events := &mockEventPublisher{}
	logger := zerolog.New(io.Discard)

	cfg := &config.Config{
		Telegram: config.TelegramConfig{BotToken: "test"},
	}

	b, _ := NewBot(tg, cfg, state, sheetsSvc, worker, events, bookingSvc, userSvc, itemSvc, nil, &logger)

	state.states[123] = &models.UserState{
		UserID:      123,
		CurrentStep: StatePhoneNumber,
		TempData: map[string]interface{}{
			"item_id":   int64(1),
			"date":      time.Now().AddDate(0, 0, 5),
			"user_name": "Test User",
		},
	}

	update := tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{ID: 123},
			Chat: &tgbotapi.Chat{ID: 123},
			Text: "89991234567",
		},
	}

	b.handleMessage(context.Background(), update)

	if state.states[123] == nil || state.states[123].CurrentStep != StateMainMenu {
		t.Errorf("expected state to be %s, but it is %v", StateMainMenu, state.states[123])
	}
}

func TestBookingFlow(t *testing.T) {
	tg := &mockTelegramService{updatesChan: make(chan tgbotapi.Update, 1)}
	state := &mockStateManager{states: make(map[int64]*models.UserState)}
	userSvc := &mockUserService{users: make(map[int64]*models.User)}
	itemSvc := &mockItemService{
		items: []models.Item{
			{ID: 1, Name: "Item 1", TotalQuantity: 1, IsActive: true},
		},
	}
	bookingSvc := &mockBookingService{available: true, bookings: make(map[int64]*models.Booking)}
	sheetsSvc := &mockSheetsWriter{}
	worker := &mockSyncWorker{}
	events := &mockEventPublisher{}
	logger := zerolog.New(io.Discard)

	cfg := &config.Config{
		Telegram: config.TelegramConfig{BotToken: "test"},
		Bot:      config.BotConfig{MaxBookingDays: 365},
	}

	b, _ := NewBot(tg, cfg, state, sheetsSvc, worker, events, bookingSvc, userSvc, itemSvc, nil, &logger)

	ctx := context.Background()
	userID := int64(123)

	// 1. Start booking
	b.handleSelectItem(ctx, tgbotapi.Update{Message: &tgbotapi.Message{From: &tgbotapi.User{ID: userID}, Chat: &tgbotapi.Chat{ID: userID}}})
	if state.states[userID].CurrentStep != StateSelectItem {
		t.Fatalf("expected state %s, got %s", StateSelectItem, state.states[userID].CurrentStep)
	}

	// 2. Select item
	b.handleCallbackQuery(ctx, tgbotapi.Update{CallbackQuery: &tgbotapi.CallbackQuery{
		From:    &tgbotapi.User{ID: userID},
		Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: userID}},
		Data:    "select_item:1",
	}})
	if state.states[userID].CurrentStep != StateWaitingDate {
		t.Fatalf("expected state %s, got %s", StateWaitingDate, state.states[userID].CurrentStep)
	}

	// 3. Enter date
	futureDate := time.Now().AddDate(0, 0, 5).Format("02.01.2006")
	b.handleMessage(ctx, tgbotapi.Update{Message: &tgbotapi.Message{
		From: &tgbotapi.User{ID: userID},
		Chat: &tgbotapi.Chat{ID: userID},
		Text: futureDate,
	}})
	if state.states[userID].CurrentStep != StateEnterName {
		t.Fatalf("expected state %s, got %s", StateEnterName, state.states[userID].CurrentStep)
	}

	// 4. Enter name
	b.handleMessage(ctx, tgbotapi.Update{Message: &tgbotapi.Message{
		From: &tgbotapi.User{ID: userID},
		Chat: &tgbotapi.Chat{ID: userID},
		Text: "Test User",
	}})
	if state.states[userID].CurrentStep != StatePhoneNumber {
		t.Fatalf("expected state %s, got %s", StatePhoneNumber, state.states[userID].CurrentStep)
	}

	// 5. Enter phone
	b.handleMessage(ctx, tgbotapi.Update{Message: &tgbotapi.Message{
		From: &tgbotapi.User{ID: userID},
		Chat: &tgbotapi.Chat{ID: userID},
		Text: "89991234567",
	}})
	if state.states[userID].CurrentStep != StateMainMenu {
		t.Fatalf("expected state %s, got %s", StateMainMenu, state.states[userID].CurrentStep)
	}

	// Check if booking was created
	if len(bookingSvc.bookings) != 1 {
		t.Fatalf("expected 1 booking, got %d", len(bookingSvc.bookings))
	}
	booking := bookingSvc.bookings[1]
	if booking.Status != models.StatusPending {
		t.Errorf("expected status %s, got %s", models.StatusPending, booking.Status)
	}

	// 6. Manager confirms
	b.confirmBooking(ctx, booking, 456)
	if booking.Status != models.StatusConfirmed {
		t.Errorf("expected status %s, got %s", models.StatusConfirmed, booking.Status)
	}

	// 7. Manager completes
	b.completeBooking(ctx, booking, 456)
	if booking.Status != models.StatusCompleted {
		t.Errorf("expected status %s, got %s", models.StatusCompleted, booking.Status)
	}
}
