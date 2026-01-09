package bot

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"bronivik/internal/config"
	"bronivik/internal/database"
	"bronivik/internal/domain"
	"bronivik/internal/models"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type botMocks struct {
	tg      *mockTelegramService
	state   *mockStateManager
	user    *mockUserService
	item    *mockItemService
	booking *mockBookingService
	sheets  *mockSheetsWriter
	worker  *mockSyncWorker
	events  *mockEventPublisher
}

func setupTestBot() (*Bot, *botMocks) {
	tg := &mockTelegramService{updatesChan: make(chan tgbotapi.Update, 1)}
	state := &mockStateManager{states: make(map[int64]*models.UserState)}
	userSvc := &mockUserService{users: make(map[int64]*models.User)}
	itemSvc := &mockItemService{
		items: []*models.Item{
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
		Managers: []int64{123},
	}

	b, _ := NewBot(tg, cfg, state, sheetsSvc, worker, events, bookingSvc, userSvc, itemSvc, nil, &logger)

	// Add manager to user service
	_ = userSvc.SaveUser(context.Background(), &models.User{TelegramID: 123, IsManager: true})

	return b, &botMocks{
		tg:      tg,
		state:   state,
		user:    userSvc,
		item:    itemSvc,
		booking: bookingSvc,
		sheets:  sheetsSvc,
		worker:  worker,
		events:  events,
	}
}

// Mocks
type mockTelegramService struct {
	domain.TelegramService
	updatesChan  chan tgbotapi.Update
	sentMessages []tgbotapi.Chattable
	mu           sync.RWMutex
}

func (m *mockTelegramService) GetUpdatesChan(config tgbotapi.UpdateConfig) tgbotapi.UpdatesChannel {
	return m.updatesChan
}

func (m *mockTelegramService) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
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
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentMessages = append(m.sentMessages, tgbotapi.NewMessage(chatID, text))
	return tgbotapi.Message{}, nil
}

func (m *mockTelegramService) SendMarkdown(chatID int64, text string) (tgbotapi.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = models.ParseModeMarkdown
	m.sentMessages = append(m.sentMessages, msg)
	return tgbotapi.Message{}, nil
}

func (m *mockTelegramService) SendWithKeyboard(chatID int64, text string, keyboard tgbotapi.ReplyKeyboardMarkup) (tgbotapi.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = keyboard
	m.sentMessages = append(m.sentMessages, msg)
	return tgbotapi.Message{}, nil
}

func (m *mockTelegramService) SendWithInlineKeyboard(
	chatID int64,
	text string,
	keyboard tgbotapi.InlineKeyboardMarkup,
) (tgbotapi.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = keyboard
	m.sentMessages = append(m.sentMessages, msg)
	return tgbotapi.Message{}, nil
}

func (m *mockTelegramService) getSentMessages() []tgbotapi.Chattable {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sentMessages
}

func (m *mockTelegramService) clearSentMessages() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentMessages = nil
}

func (m *mockTelegramService) EditMessage(
	chatID int64,
	messageID int,
	text string,
	keyboard *tgbotapi.InlineKeyboardMarkup,
) (tgbotapi.Message, error) {
	return tgbotapi.Message{}, nil
}

func (m *mockTelegramService) AnswerCallback(callbackID, text string) error {
	return nil
}

func (m *mockTelegramService) StopReceivingUpdates() {}

type mockStateManager struct {
	domain.StateManager
	states map[int64]*models.UserState
	mu     sync.RWMutex
}

func (m *mockStateManager) SetUserState(ctx context.Context, userID int64, step string, data map[string]interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.states == nil {
		m.states = make(map[int64]*models.UserState)
	}
	// Create a copy of data to avoid side effects
	dataCopy := make(map[string]interface{})
	for k, v := range data {
		dataCopy[k] = v
	}
	m.states[userID] = &models.UserState{UserID: userID, CurrentStep: step, TempData: dataCopy}
	return nil
}

func (m *mockStateManager) GetUserState(ctx context.Context, userID int64) (*models.UserState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.states == nil {
		return nil, nil
	}
	state, ok := m.states[userID]
	if !ok || state == nil {
		return nil, nil
	}
	// Return a copy to simulate database behavior
	dataCopy := make(map[string]interface{})
	for k, v := range state.TempData {
		dataCopy[k] = v
	}
	return &models.UserState{UserID: state.UserID, CurrentStep: state.CurrentStep, TempData: dataCopy}, nil
}

func (m *mockStateManager) ClearUserState(ctx context.Context, userID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.states != nil {
		delete(m.states, userID)
	}
	return nil
}

func (m *mockStateManager) getStates() map[int64]*models.UserState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cp := make(map[int64]*models.UserState)
	for k, v := range m.states {
		cp[k] = v
	}
	return cp
}

func (m *mockStateManager) CheckRateLimit(ctx context.Context, userID int64, limit int, window time.Duration) (bool, error) {
	return true, nil
}

type mockUserService struct {
	mock.Mock
	domain.UserService
	users               map[int64]*models.User
	saveError           error
	updateActivityError error
	updatePhoneError    error
	mu                  sync.RWMutex
}

func (m *mockUserService) SaveUser(ctx context.Context, user *models.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.saveError != nil {
		return m.saveError
	}
	if m.users == nil {
		m.users = make(map[int64]*models.User)
	}
	m.users[user.TelegramID] = user
	return nil
}

func (m *mockUserService) UpdateUserActivity(ctx context.Context, telegramID int64) error {
	if m.updateActivityError != nil {
		return m.updateActivityError
	}
	return nil
}

func (m *mockUserService) UpdateUserPhone(ctx context.Context, telegramID int64, phone string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.updatePhoneError != nil {
		return m.updatePhoneError
	}
	if u, ok := m.users[telegramID]; ok {
		u.Phone = phone
	}
	return nil
}

func (m *mockUserService) IsManager(userID int64) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if u, ok := m.users[userID]; ok {
		return u.IsManager
	}
	return false
}

func (m *mockUserService) IsBlacklisted(userID int64) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if u, ok := m.users[userID]; ok {
		return u.IsBlacklisted
	}
	return false
}

func (m *mockUserService) GetManagers(ctx context.Context) ([]*models.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	managers := make([]*models.User, 0, len(m.users))
	for _, u := range m.users {
		if u.IsManager {
			managers = append(managers, u)
		}
	}
	return managers, nil
}

func (m *mockUserService) GetUserBookings(ctx context.Context, userID int64) ([]*models.Booking, error) {
	if m.ExpectedCalls != nil {
		found := false
		for _, call := range m.ExpectedCalls {
			if call.Method == "GetUserBookings" {
				found = true
				break
			}
		}
		if found {
			args := m.Called(ctx, userID)
			var res []*models.Booking
			if args.Get(0) != nil {
				res = args.Get(0).([]*models.Booking)
			}
			return res, args.Error(1)
		}
	}
	return []*models.Booking{}, nil
}

func (m *mockUserService) GetUserByID(ctx context.Context, id int64) (*models.User, error) {
	if m.ExpectedCalls != nil {
		args := m.Called(ctx, id)
		if args.Get(0) == nil {
			return nil, args.Error(1)
		}
		return args.Get(0).(*models.User), args.Error(1)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, u := range m.users {
		if int64(u.ID) == id {
			return u, nil
		}
	}
	// Fallback to TelegramID if ID is not set (for tests)
	if u, ok := m.users[id]; ok {
		return u, nil
	}
	return nil, errors.New("not found")
}

func (m *mockUserService) GetAllUsers(ctx context.Context) ([]*models.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	users := make([]*models.User, 0, len(m.users))
	for _, u := range m.users {
		users = append(users, u)
	}
	return users, nil
}

func (m *mockUserService) GetActiveUsers(ctx context.Context, days int) ([]*models.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var users []*models.User
	cutoff := time.Now().AddDate(0, 0, -days)
	for _, u := range m.users {
		if u.LastActivity.After(cutoff) {
			users = append(users, u)
		}
	}
	return users, nil
}

// func (m *mockUserService) setUsers(users map[int64]*models.User) {
// 	m.mu.Lock()
// 	defer m.mu.Unlock()
// 	m.users = users
// }

func (m *mockUserService) getUsers() map[int64]*models.User {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cp := make(map[int64]*models.User)
	for k, v := range m.users {
		cp[k] = v
	}
	return cp
}

type mockItemService struct {
	domain.ItemService
	items []*models.Item
	mu    sync.RWMutex
}

func (m *mockItemService) GetActiveItems(ctx context.Context) ([]*models.Item, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.items, nil
}

func (m *mockItemService) GetItemByID(ctx context.Context, id int64) (*models.Item, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, item := range m.items {
		if item.ID == id {
			return item, nil
		}
	}
	return nil, nil
}

func (m *mockItemService) GetItemByName(ctx context.Context, name string) (*models.Item, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, item := range m.items {
		if item.Name == name {
			return item, nil
		}
	}
	return nil, errors.New("not found")
}

func (m *mockItemService) CreateItem(ctx context.Context, item *models.Item) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item.ID = int64(len(m.items) + 1)
	m.items = append(m.items, item)
	return nil
}

func (m *mockItemService) UpdateItem(ctx context.Context, item *models.Item) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, it := range m.items {
		if it.ID == item.ID {
			m.items[i] = item
			return nil
		}
	}
	return errors.New("not found")
}

func (m *mockItemService) DeactivateItem(ctx context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, it := range m.items {
		if it.ID == id {
			m.items[i].IsActive = false
			return nil
		}
	}
	return errors.New("not found")
}

func (m *mockItemService) ReorderItem(ctx context.Context, id, order int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, it := range m.items {
		if it.ID == id {
			m.items[i].SortOrder = order
			return nil
		}
	}
	return errors.New("not found")
}

func (m *mockItemService) setItems(items []*models.Item) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items = items
}

type mockBookingService struct {
	mock.Mock
	domain.BookingService
	available bool
	bookings  map[int64]*models.Booking
	mu        sync.RWMutex
}

func (m *mockBookingService) CheckAvailability(ctx context.Context, itemID int64, date time.Time) (bool, error) {
	if m.ExpectedCalls == nil {
		return m.available, nil
	}
	for _, call := range m.ExpectedCalls {
		if call.Method == "CheckAvailability" {
			args := m.Called(ctx, itemID, date)
			return args.Bool(0), args.Error(1)
		}
	}
	return m.available, nil
}

func (m *mockBookingService) CreateBooking(ctx context.Context, booking *models.Booking) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ExpectedCalls == nil {
		booking.ID = int64(len(m.bookings) + 1)
		if m.bookings == nil {
			m.bookings = make(map[int64]*models.Booking)
		}
		m.bookings[booking.ID] = booking
		return nil
	}
	for _, call := range m.ExpectedCalls {
		if call.Method == "CreateBooking" {
			args := m.Called(ctx, booking)
			return args.Error(0)
		}
	}
	booking.ID = int64(len(m.bookings) + 1)
	if m.bookings == nil {
		m.bookings = make(map[int64]*models.Booking)
	}
	m.bookings[booking.ID] = booking
	return nil
}

func (m *mockBookingService) ValidateBookingDate(date time.Time) error {
	if m.ExpectedCalls == nil {
		return nil
	}
	for _, call := range m.ExpectedCalls {
		if call.Method == "ValidateBookingDate" {
			args := m.Called(date)
			return args.Error(0)
		}
	}
	return nil
}

func (m *mockBookingService) GetDailyBookings(ctx context.Context, start, end time.Time) (map[string][]*models.Booking, error) {
	return nil, nil
}

func (m *mockBookingService) GetBookedCount(ctx context.Context, itemID int64, date time.Time) (int, error) {
	return 0, nil
}

func (m *mockBookingService) ConfirmBooking(ctx context.Context, bookingID, version, managerID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b, ok := m.bookings[bookingID]; ok {
		b.Status = models.StatusConfirmed
		return nil
	}
	return nil
}

func (m *mockBookingService) CompleteBooking(ctx context.Context, bookingID, version, managerID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b, ok := m.bookings[bookingID]; ok {
		b.Status = models.StatusCompleted
		return nil
	}
	return nil
}

func (m *mockBookingService) ReopenBooking(ctx context.Context, bookingID, version, managerID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b, ok := m.bookings[bookingID]; ok {
		b.Status = models.StatusPending
		return nil
	}
	return nil
}

func (m *mockBookingService) ChangeBookingItem(ctx context.Context, bookingID, version, newItemID, managerID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b, ok := m.bookings[bookingID]; ok {
		b.ItemID = newItemID
		b.Status = models.StatusChanged
		return nil
	}
	return nil
}

func (m *mockBookingService) RescheduleBooking(ctx context.Context, bookingID, managerID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b, ok := m.bookings[bookingID]; ok {
		b.Status = models.StatusChanged
		return nil
	}
	return nil
}

func (m *mockBookingService) GetBooking(ctx context.Context, id int64) (*models.Booking, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.bookings[id], nil
}

func (m *mockBookingService) GetBookingsByDateRange(ctx context.Context, start, end time.Time) ([]*models.Booking, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.ExpectedCalls != nil {
		found := false
		for _, call := range m.ExpectedCalls {
			if call.Method == "GetBookingsByDateRange" {
				found = true
				break
			}
		}
		if found {
			args := m.Called(ctx, start, end)
			var res []*models.Booking
			if args.Get(0) != nil {
				res = args.Get(0).([]*models.Booking)
			}
			return res, args.Error(1)
		}
	}

	var result []*models.Booking
	for _, b := range m.bookings {
		if (b.Date.After(start) || b.Date.Equal(start)) && (b.Date.Before(end) || b.Date.Equal(end)) {
			result = append(result, b)
		}
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

func (m *mockBookingService) GetAvailability(
	ctx context.Context,
	itemID int64,
	startDate time.Time,
	days int,
) ([]*models.Availability, error) {
	return []*models.Availability{{Date: startDate, Available: 1}}, nil
}

func (m *mockBookingService) RejectBooking(ctx context.Context, bookingID, version, managerID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b, ok := m.bookings[bookingID]; ok {
		b.Status = models.StatusCanceled
	}
	return nil
}

func (m *mockBookingService) getBookings() map[int64]*models.Booking {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cp := make(map[int64]*models.Booking)
	for k, v := range m.bookings {
		cp[k] = v
	}
	return cp
}

func (m *mockBookingService) setBookings(bookings map[int64]*models.Booking) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bookings = bookings
}

type mockSheetsWriter struct {
	domain.SheetsWriter
}

func (m *mockSheetsWriter) AppendBooking(ctx context.Context, booking *models.Booking) error {
	return nil
}

func (m *mockSheetsWriter) UpdateScheduleSheet(
	ctx context.Context,
	startDate, endDate time.Time,
	bookings map[string][]*models.Booking,
	items []*models.Item,
) error {
	return nil
}

func (m *mockSheetsWriter) ReplaceBookingsSheet(ctx context.Context, bookings []*models.Booking) error {
	return nil
}

func (m *mockSheetsWriter) UpdateUsersSheet(ctx context.Context, users []*models.User) error {
	return nil
}

func (m *mockSheetsWriter) UpdateBookingsSheet(ctx context.Context, bookings []*models.Booking) error {
	return nil
}

func (m *mockSheetsWriter) UpsertBooking(ctx context.Context, booking *models.Booking) error {
	return nil
}

func (m *mockSheetsWriter) UpdateBookingStatus(ctx context.Context, bookingID int64, status string) error {
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

	users := userSvc.getUsers()
	if len(users) != 1 {
		t.Errorf("expected 1 user in repo, got %d", len(users))
	}

	if users[123].Username != "testuser" {
		t.Errorf("expected username testuser, got %s", users[123].Username)
	}

	if len(tg.getSentMessages()) == 0 {
		t.Errorf("expected at least one message sent")
	}
}

func TestHandleSelectItem(t *testing.T) {
	tg := &mockTelegramService{updatesChan: make(chan tgbotapi.Update, 1)}
	state := &mockStateManager{states: make(map[int64]*models.UserState)}
	userSvc := &mockUserService{}
	itemSvc := &mockItemService{
		items: []*models.Item{
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

	b.handleMessage(context.Background(), &update)

	states := state.getStates()
	if states[123].CurrentStep != models.StateSelectItem {
		t.Errorf("expected state %s, got %s", models.StateSelectItem, states[123].CurrentStep)
	}

	if len(tg.getSentMessages()) == 0 {
		t.Errorf("expected message sent")
	}
}

func TestHandleCallbackQuery(t *testing.T) {
	tg := &mockTelegramService{updatesChan: make(chan tgbotapi.Update, 1)}
	state := &mockStateManager{states: make(map[int64]*models.UserState)}
	userSvc := &mockUserService{}
	itemSvc := &mockItemService{
		items: []*models.Item{
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

	b.handleCallbackQuery(context.Background(), &update)

	// After selecting an item, it should ask for a date
	states := state.getStates()
	if states[123].CurrentStep != models.StateWaitingDate {
		t.Errorf("expected state %s, got %s", models.StateWaitingDate, states[123].CurrentStep)
	}
}

func TestHandleCallbackQuery_BackToMain(t *testing.T) {
	tg := &mockTelegramService{updatesChan: make(chan tgbotapi.Update, 1)}
	state := &mockStateManager{states: make(map[int64]*models.UserState)}
	userSvc := &mockUserService{}
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

	_ = state.SetUserState(context.Background(), 123, "some_step", nil)

	update := tgbotapi.Update{
		CallbackQuery: &tgbotapi.CallbackQuery{
			ID:   "cb123",
			From: &tgbotapi.User{ID: 123},
			Message: &tgbotapi.Message{
				Chat: &tgbotapi.Chat{ID: 123},
			},
			Data: "back_to_main",
		},
	}

	b.handleCallbackQuery(context.Background(), &update)

	states := state.getStates()
	if states[123] == nil {
		t.Errorf("expected state to be set to main menu")
	} else if states[123].CurrentStep != models.StateMainMenu {
		t.Errorf("expected state %s, got %s", models.StateMainMenu, states[123].CurrentStep)
	}
}

func TestHandleCallbackQuery_ScheduleSelectItem(t *testing.T) {
	tg := &mockTelegramService{updatesChan: make(chan tgbotapi.Update, 1)}
	state := &mockStateManager{states: make(map[int64]*models.UserState)}
	userSvc := &mockUserService{}
	itemSvc := &mockItemService{
		items: []*models.Item{{ID: 1, Name: "Item 1"}},
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
				Chat: &tgbotapi.Chat{ID: 123},
			},
			Data: "schedule_select_item:1",
		},
	}

	b.handleCallbackQuery(context.Background(), &update)

	states := state.getStates()
	if states[123].CurrentStep != models.StateViewSchedule {
		t.Errorf("expected state %s, got %s", models.StateViewSchedule, states[123].CurrentStep)
	}
}

func TestHandleDateInput(t *testing.T) {
	b, mocks := setupTestBot()

	_ = mocks.state.SetUserState(context.Background(), 123, models.StateWaitingDate, map[string]interface{}{"item_id": int64(1)})

	futureDate := time.Now().AddDate(0, 0, 5).Format("02.01.2006")
	update := tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{ID: 123},
			Chat: &tgbotapi.Chat{ID: 123},
			Text: futureDate,
		},
	}

	mocks.booking.On("ValidateBookingDate", mock.Anything).Return(nil)

	b.handleMessage(context.Background(), &update)

	state, _ := mocks.state.GetUserState(context.Background(), 123)
	if state.CurrentStep != models.StateEnterName {
		t.Errorf("expected state %s, got %s", models.StateEnterName, state.CurrentStep)
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

	b.handleStartWithUserTracking(context.Background(), &update)

	users := userSvc.getUsers()
	user, ok := users[123]
	if !ok {
		t.Fatal("user not created in repo")
	}

	if user.Username != "testuser" || user.FirstName != "Test" {
		t.Errorf("user data mismatch: %+v", user)
	}

	states := state.getStates()
	if states[123].CurrentStep != models.StateMainMenu {
		t.Errorf("expected state %s, got %s", models.StateMainMenu, states[123].CurrentStep)
	}
}

func TestHandlePhoneReceived(t *testing.T) {
	tg := &mockTelegramService{updatesChan: make(chan tgbotapi.Update, 1)}
	state := &mockStateManager{states: make(map[int64]*models.UserState)}
	userSvc := &mockUserService{users: make(map[int64]*models.User)}
	itemSvc := &mockItemService{
		items: []*models.Item{
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

	_ = state.SetUserState(context.Background(), 123, models.StatePhoneNumber, map[string]interface{}{
		"item_id":   int64(1),
		"date":      time.Now().AddDate(0, 0, 5),
		"user_name": "Test User",
	})

	update := tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{ID: 123},
			Chat: &tgbotapi.Chat{ID: 123},
			Text: "89991234567",
		},
	}

	b.handleMessage(context.Background(), &update)

	states := state.getStates()
	if states[123] == nil || states[123].CurrentStep != models.StateMainMenu {
		t.Errorf("expected state to be %s, but it is %v", models.StateMainMenu, states[123])
	}
}

func TestBookingFlow(t *testing.T) {
	b, mocks := setupTestBot()
	ctx := context.Background()
	userID := int64(123)

	// 1. Start booking
	b.handleSelectItem(ctx, &tgbotapi.Update{Message: &tgbotapi.Message{From: &tgbotapi.User{ID: userID}, Chat: &tgbotapi.Chat{ID: userID}}})
	state, _ := mocks.state.GetUserState(ctx, userID)
	if state.CurrentStep != models.StateSelectItem {
		t.Fatalf("expected state %s, got %s", models.StateSelectItem, state.CurrentStep)
	}

	// 2. Select item
	b.handleCallbackQuery(ctx, &tgbotapi.Update{CallbackQuery: &tgbotapi.CallbackQuery{
		From:    &tgbotapi.User{ID: userID},
		Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: userID}},
		Data:    "select_item:1",
	}})
	state, _ = mocks.state.GetUserState(ctx, userID)
	if state.CurrentStep != models.StateWaitingDate {
		t.Fatalf("expected state %s, got %s", models.StateWaitingDate, state.CurrentStep)
	}

	// 3. Enter date
	futureDate := time.Now().AddDate(0, 0, 5).Format("02.01.2006")
	mocks.booking.On("ValidateBookingDate", mock.Anything).Return(nil)
	b.handleMessage(ctx, &tgbotapi.Update{Message: &tgbotapi.Message{
		From: &tgbotapi.User{ID: userID},
		Chat: &tgbotapi.Chat{ID: userID},
		Text: futureDate,
	}})
	state, _ = mocks.state.GetUserState(ctx, userID)
	if state.CurrentStep != models.StateEnterName {
		t.Fatalf("expected state %s, got %s", models.StateEnterName, state.CurrentStep)
	}

	// 4. Enter name
	b.handleMessage(ctx, &tgbotapi.Update{Message: &tgbotapi.Message{
		From: &tgbotapi.User{ID: userID},
		Chat: &tgbotapi.Chat{ID: userID},
		Text: "Test User",
	}})
	state, _ = mocks.state.GetUserState(ctx, userID)
	if state.CurrentStep != models.StatePhoneNumber {
		t.Fatalf("expected state %s, got %s", models.StatePhoneNumber, state.CurrentStep)
	}

	// 5. Enter phone
	mocks.booking.On("CheckAvailability", mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	b.handleMessage(ctx, &tgbotapi.Update{Message: &tgbotapi.Message{
		From: &tgbotapi.User{ID: userID},
		Chat: &tgbotapi.Chat{ID: userID},
		Text: "89991234567",
	}})
	state, _ = mocks.state.GetUserState(ctx, userID)
	if state.CurrentStep != models.StateMainMenu {
		t.Fatalf("expected state %s, got %s", models.StateMainMenu, state.CurrentStep)
	}

	// Check if booking was created
	bookings := mocks.booking.getBookings()
	if len(bookings) != 1 {
		t.Fatalf("expected 1 booking, got %d", len(bookings))
	}
	booking := bookings[1]
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

func TestHandleBlacklistedUser(t *testing.T) {
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

	// Mock blacklist
	_ = userSvc.SaveUser(context.Background(), &models.User{TelegramID: 123, IsBlacklisted: true})

	update := tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{ID: 123},
			Chat: &tgbotapi.Chat{ID: 123},
			Text: "/start",
		},
	}

	b.handleMessage(context.Background(), &update)

	// Should not send any messages
	if len(tg.getSentMessages()) > 0 {
		t.Errorf("expected no messages for blacklisted user, got %d", len(tg.getSentMessages()))
	}
}

func TestHandleViewSchedule(t *testing.T) {
	tg := &mockTelegramService{updatesChan: make(chan tgbotapi.Update, 1)}
	state := &mockStateManager{states: make(map[int64]*models.UserState)}
	userSvc := &mockUserService{}
	itemSvc := &mockItemService{
		items: []*models.Item{
			{ID: 1, Name: "Item 1", IsActive: true},
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
			Text: "📅 Посмотреть расписание",
		},
	}

	b.handleMessage(context.Background(), &update)

	if len(tg.sentMessages) == 0 {
		t.Errorf("expected message sent")
	}
}

func TestHandleMessage_Contacts(t *testing.T) {
	b, mocks := setupTestBot()

	update := tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{ID: 123},
			Chat: &tgbotapi.Chat{ID: 123},
			Text: "📞 Контакты менеджеров",
		},
	}

	b.handleMessage(context.Background(), &update)

	if len(mocks.tg.sentMessages) == 0 {
		t.Errorf("expected message sent")
	}
}

func TestHandleMessage_MyBookings(t *testing.T) {
	b, mocks := setupTestBot()

	update := tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{ID: 123},
			Chat: &tgbotapi.Chat{ID: 123},
			Text: "📊 Мои заявки",
		},
	}

	b.handleMessage(context.Background(), &update)

	if len(mocks.tg.sentMessages) == 0 {
		t.Errorf("expected message sent")
	}
}

func TestHandleManagerCommand_AllBookings(t *testing.T) {
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

	// Set user as manager
	err := userSvc.SaveUser(context.Background(), &models.User{TelegramID: 123, IsManager: true})
	require.NoError(t, err)

	update := tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{ID: 123},
			Chat: &tgbotapi.Chat{ID: 123},
			Text: "👨‍💼 Все заявки",
		},
	}

	b.handleMessage(context.Background(), &update)

	if len(tg.getSentMessages()) == 0 {
		t.Errorf("expected message sent to manager")
	}
}

func TestHandleManagerCallback_Confirm(t *testing.T) {
	tg := &mockTelegramService{updatesChan: make(chan tgbotapi.Update, 1)}
	state := &mockStateManager{states: make(map[int64]*models.UserState)}
	userSvc := &mockUserService{users: make(map[int64]*models.User)}
	itemSvc := &mockItemService{}
	bookingSvc := &mockBookingService{
		bookings: map[int64]*models.Booking{
			1: {ID: 1, Status: models.StatusPending, UserID: 456},
		},
	}
	sheetsSvc := &mockSheetsWriter{}
	worker := &mockSyncWorker{}
	events := &mockEventPublisher{}
	logger := zerolog.New(io.Discard)

	cfg := &config.Config{
		Telegram: config.TelegramConfig{BotToken: "test"},
	}

	b, _ := NewBot(tg, cfg, state, sheetsSvc, worker, events, bookingSvc, userSvc, itemSvc, nil, &logger)

	// Set user as manager
	err := userSvc.SaveUser(context.Background(), &models.User{TelegramID: 123, IsManager: true})
	require.NoError(t, err)

	update := tgbotapi.Update{
		CallbackQuery: &tgbotapi.CallbackQuery{
			ID:   "cb123",
			From: &tgbotapi.User{ID: 123},
			Message: &tgbotapi.Message{
				Chat:      &tgbotapi.Chat{ID: 123},
				MessageID: 789,
			},
			Data: "confirm_1",
		},
	}

	b.handleCallbackQuery(context.Background(), &update)

	bookings := bookingSvc.getBookings()
	if bookings[1].Status != models.StatusConfirmed {
		t.Errorf("expected status %s, got %s", models.StatusConfirmed, bookings[1].Status)
	}
}

func TestHandleCancel(t *testing.T) {
	tg := &mockTelegramService{updatesChan: make(chan tgbotapi.Update, 1)}
	state := &mockStateManager{states: make(map[int64]*models.UserState)}
	userSvc := &mockUserService{}
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

	_ = state.SetUserState(context.Background(), 123, models.StateWaitingDate, nil)

	update := tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{ID: 123},
			Chat: &tgbotapi.Chat{ID: 123},
			Text: "❌ Отмена",
		},
	}

	b.handleMessage(context.Background(), &update)

	s, _ := state.GetUserState(context.Background(), 123)
	if s == nil {
		t.Errorf("expected state to be set to main_menu")
	} else if s.CurrentStep != models.StateMainMenu {
		t.Errorf("expected state %s, got %s", models.StateMainMenu, s.CurrentStep)
	}
}

func TestHandleBack(t *testing.T) {
	tests := []struct {
		name         string
		fromStep     string
		expectedStep string
	}{
		{"ToDate", models.StateEnterName, models.StateWaitingDate},
		{"ToName", models.StatePhoneNumber, models.StateEnterName},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, mocks := setupTestBot()

			err := mocks.state.SetUserState(context.Background(), 123, tt.fromStep, map[string]interface{}{"item_id": int64(1)})
			require.NoError(t, err)

			update := tgbotapi.Update{
				Message: &tgbotapi.Message{
					From: &tgbotapi.User{ID: 123},
					Chat: &tgbotapi.Chat{ID: 123},
					Text: "⬅️ Назад",
				},
			}

			b.handleMessage(context.Background(), &update)

			s, _ := mocks.state.GetUserState(context.Background(), 123)
			assert.Equal(t, tt.expectedStep, s.CurrentStep)
		})
	}
}

func TestGetErrorMessage(t *testing.T) {
	b := &Bot{}
	tests := []struct {
		err      error
		expected string
	}{
		{nil, ""},
		{database.ErrNotAvailable, "⚠️ Извините, этот аппарат уже забронирован на выбранную дату. " +
			"Пожалуйста, выберите другое время или аппарат."},
		{database.ErrPastDate, "⚠️ Нельзя создавать бронирование на прошедшую дату."},
		{database.ErrDateTooFar, "⚠️ Вы не можете бронировать так далеко в будущем. Пожалуйста, выберите более раннюю дату."},
		{database.ErrConcurrentModification, "⚠️ Произошла ошибка при сохранении (конфликт версий). Пожалуйста, попробуйте еще раз."},
		{errors.New("unknown"), "❌ Произошла ошибка при обработке вашего запроса. Пожалуйста, попробуйте позже или обратитесь к менеджеру."},
	}

	for _, tt := range tests {
		if got := b.getErrorMessage(tt.err); got != tt.expected {
			t.Errorf("getErrorMessage(%v) = %v, want %v", tt.err, got, tt.expected)
		}
	}
}

func TestBotWrapper(t *testing.T) {
	botAPI := &tgbotapi.BotAPI{Self: tgbotapi.User{UserName: "test"}}
	wrapper := NewBotWrapper(botAPI)

	if wrapper.GetSelf().UserName != "test" {
		t.Errorf("expected test, got %s", wrapper.GetSelf().UserName)
	}
}

func TestManagerBookingFlow(t *testing.T) {
	b, mocks := setupTestBot()
	ctx := context.Background()
	managerID := int64(123) // From config.Managers

	// 1. Start Manager Booking
	update := tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{ID: managerID},
			Chat: &tgbotapi.Chat{ID: managerID},
			Text: "/start_booking",
		},
	}

	b.startManagerBooking(ctx, &update)

	state := b.getUserState(ctx, managerID)
	assert.NotNil(t, state)
	assert.Equal(t, models.StateManagerWaitingClientName, state.CurrentStep)
	assert.True(t, state.TempData["is_manager_booking"].(bool))
	assert.Len(t, mocks.tg.getSentMessages(), 1)

	// 2. Handle Client Name
	mocks.tg.clearSentMessages()
	b.handleManagerClientName(ctx, &update, "John Doe", state)

	state = b.getUserState(ctx, managerID)
	assert.Equal(t, models.StateManagerWaitingClientPhone, state.CurrentStep)
	assert.Equal(t, "John Doe", state.TempData["client_name"])

	// 3. Handle Client Phone
	mocks.tg.clearSentMessages()
	b.handleManagerClientPhone(ctx, &update, "+79991234567", state)

	state = b.getUserState(ctx, managerID)
	assert.Equal(t, models.StateManagerWaitingItemSelection, state.CurrentStep)
	assert.Equal(t, "79991234567", state.TempData["client_phone"])

	// 4. Handle Item Selection
	mocks.tg.clearSentMessages()
	callbackUpdate := tgbotapi.Update{
		CallbackQuery: &tgbotapi.CallbackQuery{
			From: &tgbotapi.User{ID: managerID},
			Message: &tgbotapi.Message{
				Chat:      &tgbotapi.Chat{ID: managerID},
				MessageID: 456,
			},
			Data: "manager_select_item:1",
		},
	}
	b.handleManagerItemSelection(ctx, &callbackUpdate)

	state = b.getUserState(ctx, managerID)
	assert.Equal(t, models.StateManagerWaitingDateType, state.CurrentStep)
	assert.Equal(t, int64(1), state.TempData["item_id"])

	// 5. Handle Date Type (Single)
	mocks.tg.clearSentMessages()
	callbackUpdate.CallbackQuery.Data = "manager_single_date"
	b.handleManagerDateType(ctx, &callbackUpdate, "single")

	state = b.getUserState(ctx, managerID)
	assert.Equal(t, models.StateManagerWaitingSingleDate, state.CurrentStep)
	assert.Equal(t, "single", state.TempData["date_type"])

	// 6. Handle Single Date
	mocks.tg.clearSentMessages()
	dateStr := time.Now().AddDate(0, 0, 1).Format("02.01.2006")
	mocks.booking.On("ValidateBookingDate", mock.Anything).Return(nil)
	b.handleManagerSingleDate(ctx, &update, dateStr, state)

	state = b.getUserState(ctx, managerID)
	assert.Equal(t, models.StateManagerWaitingComment, state.CurrentStep)
	assert.NotEmpty(t, state.TempData["dates"])

	// 7. Handle Comment
	mocks.tg.clearSentMessages()
	b.handleManagerComment(ctx, &update, "Test comment", state)

	state = b.getUserState(ctx, managerID)
	assert.Equal(t, models.StateManagerConfirmBooking, state.CurrentStep)
	assert.Equal(t, "Test comment", state.TempData["comment"])

	// 8. Create Bookings
	mocks.tg.clearSentMessages()
	mocks.booking.On("CheckAvailability", mock.Anything, int64(1), mock.Anything).Return(true, nil)
	mocks.booking.On("CreateBooking", mock.Anything, mock.Anything).Return(nil)
	b.createManagerBookings(ctx, &update, state)

	state = b.getUserState(ctx, managerID)
	assert.NotNil(t, state)
	assert.Equal(t, models.StateMainMenu, state.CurrentStep)
}

func TestManagerBookingFlow_DateRange(t *testing.T) {
	b, mocks := setupTestBot()
	ctx := context.Background()
	managerID := int64(123)

	// Setup state for range selection
	b.setUserState(ctx, managerID, models.StateManagerWaitingDateType, map[string]interface{}{
		"item_id":      int64(1),
		"client_name":  "John Doe",
		"client_phone": "79991234567",
	})

	update := tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{ID: managerID},
			Chat: &tgbotapi.Chat{ID: managerID},
		},
		CallbackQuery: &tgbotapi.CallbackQuery{
			From: &tgbotapi.User{ID: managerID},
			Message: &tgbotapi.Message{
				Chat:      &tgbotapi.Chat{ID: managerID},
				MessageID: 456,
			},
		},
	}

	// 1. Select Range
	b.handleManagerDateType(ctx, &update, "range")
	state := b.getUserState(ctx, managerID)
	assert.Equal(t, models.StateManagerWaitingStartDate, state.CurrentStep)

	// 2. Handle Start Date
	startDate := time.Now().AddDate(0, 0, 1)
	mocks.booking.On("ValidateBookingDate", mock.Anything).Return(nil)
	b.handleManagerStartDate(ctx, &update, startDate.Format("02.01.2006"), state)
	state = b.getUserState(ctx, managerID)
	assert.Equal(t, models.StateManagerWaitingEndDate, state.CurrentStep)

	// 3. Handle End Date
	endDate := startDate.AddDate(0, 0, 2) // 3 days total
	b.handleManagerEndDate(ctx, &update, endDate.Format("02.01.2006"), state)
	state = b.getUserState(ctx, managerID)
	assert.Equal(t, models.StateManagerWaitingComment, state.CurrentStep)
	dates := state.GetDates("dates")
	assert.Len(t, dates, 3)

	// 4. Handle Comment & Create
	b.handleManagerComment(ctx, &update, "Range comment", state)
	mocks.booking.On("CheckAvailability", mock.Anything, int64(1), mock.Anything).Return(true, nil)
	mocks.booking.On("CreateBooking", mock.Anything, mock.Anything).Return(nil)
	b.createManagerBookings(ctx, &update, state)

	state = b.getUserState(ctx, managerID)
	assert.NotNil(t, state)
	assert.Equal(t, models.StateMainMenu, state.CurrentStep)
}

func TestManagerItemsCommands(t *testing.T) {
	b, mocks := setupTestBot()
	ctx := context.Background()
	managerID := int64(123)

	update := tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{ID: managerID},
			Chat: &tgbotapi.Chat{ID: managerID},
		},
	}

	// 1. Add Item
	update.Message.Text = "/add_item NewItem 5"
	b.handleAddItemCommand(ctx, &update)
	assert.Len(t, mocks.tg.getSentMessages(), 1)
	assert.Contains(t, mocks.tg.getSentMessages()[0].(tgbotapi.MessageConfig).Text, "✅ Аппарат 'NewItem' добавлен")

	// 2. List Items
	mocks.tg.clearSentMessages()
	mocks.item.setItems([]*models.Item{{ID: 1, Name: "Item 1", TotalQuantity: 10, SortOrder: 1}})
	update.Message.Text = "/list_items"
	b.handleListItemsCommand(ctx, &update)
	assert.Len(t, mocks.tg.getSentMessages(), 1)
	assert.Contains(t, mocks.tg.getSentMessages()[0].(tgbotapi.MessageConfig).Text, "📋 Список активных аппаратов")

	// 3. Edit Item
	mocks.tg.clearSentMessages()
	update.Message.Text = "/edit_item Item 1 20"
	b.handleEditItemCommand(ctx, &update)
	assert.Len(t, mocks.tg.getSentMessages(), 1)
	assert.Contains(t, mocks.tg.getSentMessages()[0].(tgbotapi.MessageConfig).Text, "✅ Аппарат 'Item 1' обновлён")

	// 4. Set Item Order
	mocks.tg.clearSentMessages()
	update.Message.Text = "/set_item_order Item 1 5"
	b.handleSetItemOrderCommand(ctx, &update)
	assert.Len(t, mocks.tg.getSentMessages(), 1)
	assert.Contains(t, mocks.tg.getSentMessages()[0].(tgbotapi.MessageConfig).Text, "↕️ Порядок 'Item 1' установлен на 5")

	// 5. Move Item Up/Down
	mocks.tg.clearSentMessages()
	update.Message.Text = "/move_item_up Item 1"
	b.handleMoveItemCommand(ctx, &update, -1)
	assert.Len(t, mocks.tg.getSentMessages(), 1)
	assert.Contains(t, mocks.tg.getSentMessages()[0].(tgbotapi.MessageConfig).Text, "перемещён вверх")

	// 6. Disable Item
	mocks.tg.clearSentMessages()
	update.Message.Text = "/disable_item Item 1"
	b.handleDisableItemCommand(ctx, &update)
	assert.Len(t, mocks.tg.getSentMessages(), 1)
	assert.Contains(t, mocks.tg.getSentMessages()[0].(tgbotapi.MessageConfig).Text, "деактивирован")
}

func TestExportToExcel(t *testing.T) {
	b, mocks := setupTestBot()
	ctx := context.Background()

	// Setup temp export path
	tmpDir, err := os.MkdirTemp("", "bronivik_export")
	assert.NoError(t, err)
	defer os.RemoveAll(tmpDir)
	b.config.Exports.Path = tmpDir

	startDate := time.Now()
	endDate := startDate.AddDate(0, 0, 7)

	// Mock data
	mocks.item.setItems([]*models.Item{{ID: 1, Name: "Item 1", TotalQuantity: 5}})
	mocks.booking.setBookings(map[int64]*models.Booking{
		1: {ID: 1, ItemID: 1, Date: startDate, Status: models.StatusConfirmed, UserName: "User 1"},
	})

	filePath, err := b.exportToExcel(ctx, startDate, endDate)
	assert.NoError(t, err)
	assert.NotEmpty(t, filePath)
	assert.FileExists(t, filePath)
}

func TestExportUsersToExcel(t *testing.T) {
	b, _ := setupTestBot()
	ctx := context.Background()

	// Setup temp export path
	tmpDir, err := os.MkdirTemp("", "bronivik_users_export")
	assert.NoError(t, err)
	defer os.RemoveAll(tmpDir)
	b.config.Exports.Path = tmpDir

	users := []*models.User{
		{ID: 1, TelegramID: 123, Username: "user1", FirstName: "First", LastName: "Last", Phone: "79991234567"},
	}

	filePath, err := b.exportUsersToExcel(ctx, users)
	assert.NoError(t, err)
	assert.NotEmpty(t, filePath)
	assert.FileExists(t, filePath)
}
func TestManagerStats(t *testing.T) {
	b, mocks := setupTestBot()

	// Setup temp export path
	tmpDir, err := os.MkdirTemp("", "bronivik_stats_export")
	assert.NoError(t, err)
	defer os.RemoveAll(tmpDir)
	b.config.Exports.Path = tmpDir

	// Add some users
	err = mocks.user.SaveUser(context.Background(), &models.User{
		TelegramID: 1, FirstName: "User 1", LastActivity: time.Now(),
	})
	require.NoError(t, err)
	err = mocks.user.SaveUser(context.Background(), &models.User{
		TelegramID: 2, FirstName: "User 2",
		LastActivity: time.Now().AddDate(0, 0, -40), IsBlacklisted: true,
	})
	require.NoError(t, err)

	// Add some bookings
	mocks.booking.setBookings(map[int64]*models.Booking{
		1: {ID: 1, ItemName: "Item 1", Status: models.StatusConfirmed, Date: time.Now()},
		2: {ID: 2, ItemName: "Item 1", Status: models.StatusPending, Date: time.Now()},
	})

	// Test getUserStats
	update := tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{ID: 123}, // Manager
			Chat: &tgbotapi.Chat{ID: 123},
			Text: "/stats",
		},
	}

	b.getUserStats(context.Background(), &update)
	assert.True(t, len(mocks.tg.getSentMessages()) > 0)
	sentMsgs := mocks.tg.getSentMessages()
	msg := sentMsgs[len(sentMsgs)-1].(tgbotapi.MessageConfig)
	assert.Contains(t, msg.Text, "Статистика")
	assert.Contains(t, msg.Text, "Всего: *3*") // 123 (manager), 1, 2
	assert.Contains(t, msg.Text, "В черном списке: *1*")

	// Test handleExportUsers
	callbackUpdate := tgbotapi.Update{
		CallbackQuery: &tgbotapi.CallbackQuery{
			From: &tgbotapi.User{ID: 123},
			Message: &tgbotapi.Message{
				Chat: &tgbotapi.Chat{ID: 123},
			},
			Data: "export_users",
		},
	}

	b.handleExportUsers(context.Background(), &callbackUpdate)
	// Should send a document
	foundDoc := false
	for _, m := range mocks.tg.getSentMessages() {
		if _, ok := m.(tgbotapi.DocumentConfig); ok {
			foundDoc = true
			break
		}
	}
	assert.True(t, foundDoc)
}

func TestRemindersExtended(t *testing.T) {
	b, mocks := setupTestBot()
	ctx := context.Background()
	tomorrow := time.Now().Add(24 * time.Hour).Truncate(24 * time.Hour)

	// Case 1: Booking service error
	mocks.booking.On("GetBookingsByDateRange", ctx, tomorrow, tomorrow).Return(nil, errors.New("db error")).Once()
	b.sendTomorrowReminders(ctx)

	// Case 2: Status not for reminder
	bookingCanceled := models.Booking{ID: 1, UserID: 1, Status: models.StatusCanceled, Date: tomorrow}
	mocks.booking.On("GetBookingsByDateRange", ctx, tomorrow, tomorrow).Return([]*models.Booking{&bookingCanceled}, nil).Once()
	b.sendTomorrowReminders(ctx)

	// Case 3: User service error
	bookingPending := models.Booking{ID: 2, UserID: 1, Status: models.StatusPending, Date: tomorrow}
	mocks.booking.On("GetBookingsByDateRange", ctx, tomorrow, tomorrow).Return([]*models.Booking{&bookingPending}, nil).Once()
	mocks.user.On("GetUserByID", ctx, int64(1)).Return(nil, errors.New("not found")).Once()
	b.sendTomorrowReminders(ctx)

	// Case 4: User without TelegramID
	mocks.booking.On("GetBookingsByDateRange", ctx, tomorrow, tomorrow).Return([]*models.Booking{&bookingPending}, nil).Once()
	mocks.user.On("GetUserByID", ctx, int64(1)).Return(&models.User{TelegramID: 0}, nil).Once()
	b.sendTomorrowReminders(ctx)
}

func TestTimeUntilNextHour(t *testing.T) {
	d := timeUntilNextHour(10)
	assert.True(t, d >= 0)
	assert.True(t, d <= 25*time.Hour)
}

func TestReminders(t *testing.T) {
	b, mocks := setupTestBot()
	ctx := context.Background()

	tomorrow := time.Now().Add(24 * time.Hour).Truncate(24 * time.Hour)

	// Add a user
	err := mocks.user.SaveUser(ctx, &models.User{
		TelegramID: 1,
		FirstName:  "User 1",
	})
	require.NoError(t, err)

	// Add a booking for tomorrow
	mocks.booking.setBookings(map[int64]*models.Booking{
		1: {
			ID:       1,
			UserID:   1,
			ItemName: "Item 1",
			Status:   models.StatusConfirmed,
			Date:     tomorrow,
		},
	})

	// Run reminders
	b.sendTomorrowReminders(ctx)

	// Check if message was sent
	sentMsgs := mocks.tg.getSentMessages()
	assert.True(t, len(sentMsgs) > 0)
	msg := sentMsgs[len(sentMsgs)-1].(tgbotapi.MessageConfig)
	assert.Equal(t, int64(1), msg.ChatID)
	assert.Contains(t, msg.Text, "Напоминание")
	assert.Contains(t, msg.Text, "Item 1")
}
func TestMiddleware(t *testing.T) {
	b, _ := setupTestBot()

	// Test withRecovery
	assert.NotPanics(t, func() {
		b.withRecovery(func() {
			panic("test panic")
		})
	})

	// Test trackActivity
	b.trackActivity(123)
	// Wait a bit for the goroutine
	time.Sleep(50 * time.Millisecond)
}

func TestStop(t *testing.T) {
	b, _ := setupTestBot()
	b.Stop()
}

func TestSync(t *testing.T) {
	b, mocks := setupTestBot()
	ctx := context.Background()

	// Mock bookings
	mocks.booking.bookings[1] = &models.Booking{ID: 1, Date: time.Now()}

	b.SyncBookingsToSheets(ctx)
}

func TestMetricsUpdate(t *testing.T) {
	b, _ := setupTestBot()
	b.metrics = NewMetrics()
	b.updateGaugeMetrics(context.Background())
}
func TestManagerBookingActions(t *testing.T) {
	b, mocks := setupTestBot()
	ctx := context.Background()

	// Mock booking
	booking := &models.Booking{
		ID:       1,
		UserID:   1,
		ItemID:   1,
		ItemName: "Item 1",
		Status:   models.StatusConfirmed,
		Date:     time.Now(),
	}
	mocks.booking.setBookings(map[int64]*models.Booking{1: booking})

	// Test showManagerBookingDetail
	update := tgbotapi.Update{
		Message: &tgbotapi.Message{
			Chat: &tgbotapi.Chat{ID: 123},
		},
	}
	b.showManagerBookingDetail(ctx, &update, 1)
	assert.True(t, len(mocks.tg.getSentMessages()) > 0)

	// Test reopenBooking
	b.reopenBooking(ctx, booking, 123)
	assert.Equal(t, models.StatusPending, booking.Status)

	// Test completeBooking
	b.completeBooking(ctx, booking, 123)
	assert.Equal(t, models.StatusCompleted, booking.Status)

	// Test confirmBooking
	booking.Status = models.StatusPending
	b.confirmBooking(ctx, booking, 123)
	assert.Equal(t, models.StatusConfirmed, booking.Status)

	// Test rejectBooking
	b.rejectBooking(ctx, booking, 123)
	assert.Equal(t, models.StatusCanceled, booking.Status)

	// Test startChangeItem
	b.startChangeItem(ctx, booking, 123)
	assert.True(t, len(mocks.tg.sentMessages) > 0)

	// Test handleChangeItem
	callbackUpdate := tgbotapi.Update{
		CallbackQuery: &tgbotapi.CallbackQuery{
			From: &tgbotapi.User{ID: 123},
			Message: &tgbotapi.Message{
				Chat: &tgbotapi.Chat{ID: 123},
			},
			Data: "change_to_1_1",
		},
	}
	b.handleChangeItem(ctx, &callbackUpdate)
	assert.Equal(t, models.StatusChanged, booking.Status)

	// Test rescheduleBooking
	b.rescheduleBooking(ctx, booking, 123)
	assert.Equal(t, models.StatusChanged, booking.Status)

	// Test handleCallButton
	callbackUpdate.CallbackQuery.Data = "call_booking:1"
	b.handleCallButton(ctx, &callbackUpdate)
	assert.True(t, len(mocks.tg.sentMessages) > 0)
}

func TestManagerMoreActions(t *testing.T) {
	b, mocks := setupTestBot()
	ctx := context.Background()
	managerID := int64(123)

	// Test non-manager access to startManagerBooking
	b.startManagerBooking(ctx, &tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{ID: 999},
			Chat: &tgbotapi.Chat{ID: 999},
		},
	})
	assert.Len(t, mocks.tg.sentMessages, 0)

	// Test handleManagerItemSelection error cases
	b.handleManagerItemSelection(ctx, &tgbotapi.Update{
		CallbackQuery: &tgbotapi.CallbackQuery{
			Data: "manager_select_item:invalid",
			From: &tgbotapi.User{ID: managerID},
			Message: &tgbotapi.Message{
				Chat: &tgbotapi.Chat{ID: managerID},
			},
		},
	})
	b.handleManagerItemSelection(ctx, &tgbotapi.Update{
		CallbackQuery: &tgbotapi.CallbackQuery{
			Data: "manager_select_item:999",
			From: &tgbotapi.User{ID: managerID},
			Message: &tgbotapi.Message{
				Chat: &tgbotapi.Chat{ID: managerID},
			},
		},
	})

	// Test handleManagerSingleDate error cases
	state := &models.UserState{UserID: managerID}
	b.handleManagerSingleDate(ctx, &tgbotapi.Update{
		Message: &tgbotapi.Message{
			Chat: &tgbotapi.Chat{ID: managerID},
			From: &tgbotapi.User{ID: managerID},
		},
	}, "invalid date", state)

	// Test handleManagerEndDate edge cases
	state.TempData = map[string]interface{}{"start_date": time.Now()}
	b.handleManagerEndDate(ctx, &tgbotapi.Update{
		Message: &tgbotapi.Message{
			Chat: &tgbotapi.Chat{ID: managerID},
			From: &tgbotapi.User{ID: managerID},
		},
	}, time.Now().AddDate(0, 0, -1).Format("02.01.2006"), state)
	b.handleManagerEndDate(ctx, &tgbotapi.Update{
		Message: &tgbotapi.Message{
			Chat: &tgbotapi.Chat{ID: managerID},
			From: &tgbotapi.User{ID: managerID},
		},
	}, time.Now().AddDate(0, 0, 40).Format("02.01.2006"), state)

	// Test notifyManagers
	b.notifyManagers(&models.Booking{ID: 1, ItemName: "Item 1", Date: time.Now()})
	assert.True(t, len(mocks.tg.sentMessages) > 0)
}

func TestUtilsMoreCoverage(t *testing.T) {
	b, mocks := setupTestBot()
	ctx := context.Background()

	// Test showAvailableItems
	b.showAvailableItems(ctx, &tgbotapi.Update{
		Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 123}},
	})
	assert.True(t, len(mocks.tg.getSentMessages()) > 0)

	// Test handleSpecificDateInput
	b.setUserState(ctx, 123, models.StateWaitingSpecificDate, map[string]interface{}{"item_id": int64(1)})
	b.handleSpecificDateInput(ctx, &tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{ID: 123}, Chat: &tgbotapi.Chat{ID: 123},
		},
	}, time.Now().Format("02.01.2006"))
	assert.True(t, len(mocks.tg.getSentMessages()) > 1)

	// Test showMonthScheduleForItem
	b.showMonthScheduleForItem(ctx, &tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{ID: 123}, Chat: &tgbotapi.Chat{ID: 123},
		},
	})
	assert.True(t, len(mocks.tg.getSentMessages()) > 2)

	// Test restoreStateOrRestart failures
	b.restoreStateOrRestart(ctx, &tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{ID: 999}, Chat: &tgbotapi.Chat{ID: 999},
		},
	}, "missing_field")

	// Test handleCustomInput back buttons
	b.setUserState(ctx, 123, models.StateEnterName, map[string]interface{}{"item_id": int64(1)})
	b.handleCustomInput(ctx, &tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{ID: 123}, Chat: &tgbotapi.Chat{ID: 123}, Text: "⬅️ Назад",
		},
	}, b.getUserState(ctx, 123))

	b.setUserState(ctx, 123, models.StatePhoneNumber, map[string]interface{}{"item_id": int64(1)})
	b.handleCustomInput(ctx, &tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{ID: 123}, Chat: &tgbotapi.Chat{ID: 123}, Text: "⬅️ Назад",
		},
	}, b.getUserState(ctx, 123))

	b.setUserState(ctx, 123, models.StateWaitingDate, map[string]interface{}{"item_id": int64(1)})
	b.handleCustomInput(ctx, &tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{ID: 123}, Chat: &tgbotapi.Chat{ID: 123}, Text: "⬅️ Назад",
		},
	}, b.getUserState(ctx, 123))

	// Test handleContactReceived
	b.setUserState(ctx, 123, models.StatePhoneNumber, map[string]interface{}{"item_id": int64(1), "date": time.Now()})
	b.handleContactReceived(ctx, &tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{ID: 123}, Chat: &tgbotapi.Chat{ID: 123},
			Contact: &tgbotapi.Contact{PhoneNumber: "1234567890"},
		},
	})
}

func TestPagination(t *testing.T) {
	b, mocks := setupTestBot()
	ctx := context.Background()

	// Test renderPaginatedItems
	params := PaginationParams{
		Ctx:          ctx,
		ChatID:       123,
		MessageID:    0,
		Page:         0,
		Title:        "Test Items",
		ItemPrefix:   "item_",
		PagePrefix:   "page_",
		BackCallback: "back",
		ShowCapacity: true,
	}

	b.renderPaginatedItems(&params)
	assert.True(t, len(mocks.tg.getSentMessages()) > 0)

	// Test renderPaginatedBookings
	bookings := []*models.Booking{
		{ID: 1, UserName: "User 1", ItemName: "Item 1", Date: time.Now(), Status: models.StatusConfirmed},
		{ID: 2, UserName: "User 2", ItemName: "Item 2", Date: time.Now(), Status: models.StatusPending},
	}

	params.Title = "Test Bookings"
	params.ItemPrefix = "booking_"
	b.renderPaginatedBookings(&params, bookings)
	assert.True(t, len(mocks.tg.getSentMessages()) > 1)
}

func TestSyncBookingsToSheets(t *testing.T) {
	b, mocks := setupTestBot()
	ctx := context.Background()

	// Mock bookings
	mocks.booking.setBookings(map[int64]*models.Booking{
		1: {
			ID: 1, UserID: 1, ItemID: 1, Date: time.Now(), Status: models.StatusConfirmed,
			UserName: "User 1", Phone: "123", ItemName: "Item 1",
		},
	})

	b.SyncBookingsToSheets(ctx)
	// Should not panic and call sheets service
}

func TestSyncScheduleToSheets(t *testing.T) {
	b, mocks := setupTestBot()
	ctx := context.Background()

	// Mock bookings
	mocks.booking.setBookings(map[int64]*models.Booking{
		1: {
			ID: 1, UserID: 1, ItemID: 1, Date: time.Now(), Status: models.StatusConfirmed,
			UserName: "User 1", Phone: "123", ItemName: "Item 1",
		},
	})

	b.SyncScheduleToSheets(ctx)
	// Should not panic and call sheets service
}

func TestUserHandlersErrors(t *testing.T) {
	b, mocks := setupTestBot()
	ctx := context.Background()

	// Test handleStartWithUserTracking with user service error
	mocks.user.saveError = errors.New("save user error")
	update := tgbotapi.Update{
		Message: &tgbotapi.Message{
			Chat: &tgbotapi.Chat{ID: 123},
			From: &tgbotapi.User{ID: 123, UserName: "testuser"},
		},
	}
	b.handleStartWithUserTracking(ctx, &update)
	// Should not panic despite error

	// Test updateUserActivity with error
	mocks.user.updateActivityError = errors.New("update activity error")
	b.updateUserActivity(123)
	// Should not panic

	// Test updateUserPhone with error
	mocks.user.updatePhoneError = errors.New("update phone error")
	b.updateUserPhone(123, "+1234567890")
	// Should not panic
}
