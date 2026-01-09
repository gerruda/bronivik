package domain

import (
	"context"
	"time"

	"bronivik/internal/models"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Repository interface {
	GetBooking(ctx context.Context, id int64) (*models.Booking, error)
	CreateBooking(ctx context.Context, booking *models.Booking) error
	CreateBookingWithLock(ctx context.Context, booking *models.Booking) error
	UpdateBookingStatus(ctx context.Context, id int64, status string) error
	UpdateBookingStatusWithVersion(ctx context.Context, id int64, version int64, status string) error
	GetBookingsByDateRange(ctx context.Context, start, end time.Time) ([]*models.Booking, error)
	CheckAvailability(ctx context.Context, itemID int64, date time.Time) (bool, error)
	GetAvailabilityForPeriod(ctx context.Context, itemID int64, startDate time.Time, days int) ([]*models.Availability, error)
	GetActiveItems(ctx context.Context) ([]*models.Item, error)
	GetItemByID(ctx context.Context, id int64) (*models.Item, error)
	GetItemByName(ctx context.Context, name string) (*models.Item, error)
	CreateItem(ctx context.Context, item *models.Item) error
	UpdateItem(ctx context.Context, item *models.Item) error
	DeactivateItem(ctx context.Context, id int64) error
	ReorderItem(ctx context.Context, id int64, newOrder int64) error
	GetAllUsers(ctx context.Context) ([]*models.User, error)
	GetUserByTelegramID(ctx context.Context, telegramID int64) (*models.User, error)
	GetUserByID(ctx context.Context, id int64) (*models.User, error)
	CreateOrUpdateUser(ctx context.Context, user *models.User) error
	UpdateUserActivity(ctx context.Context, telegramID int64) error
	UpdateUserPhone(ctx context.Context, telegramID int64, phone string) error
	GetDailyBookings(ctx context.Context, start, end time.Time) (map[string][]*models.Booking, error)
	GetBookedCount(ctx context.Context, itemID int64, date time.Time) (int, error)
	GetBookingWithAvailability(ctx context.Context, id int64, newItemID int64) (*models.Booking, bool, error)
	UpdateBookingItemAndStatusWithVersion(ctx context.Context, id int64, version int64, itemID int64, itemName string, status string) error
	SetItems(items []*models.Item)
	GetActiveUsers(ctx context.Context, days int) ([]*models.User, error)
	GetUsersByManagerStatus(ctx context.Context, isManager bool) ([]*models.User, error)
	GetUserBookings(ctx context.Context, userID int64) ([]*models.Booking, error)
}

type StateRepository interface {
	GetState(ctx context.Context, userID int64) (*models.UserState, error)
	SetState(ctx context.Context, state *models.UserState) error
	ClearState(ctx context.Context, userID int64) error
	CheckRateLimit(ctx context.Context, userID int64, limit int, window time.Duration) (bool, error)
}

type StateManager interface {
	GetUserState(ctx context.Context, userID int64) (*models.UserState, error)
	SetUserState(ctx context.Context, userID int64, step string, data map[string]interface{}) error
	ClearUserState(ctx context.Context, userID int64) error
	CheckRateLimit(ctx context.Context, userID int64, limit int, window time.Duration) (bool, error)
}

type EventPublisher interface {
	PublishJSON(eventType string, payload interface{}) error
}

type TelegramSender interface {
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
	Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error)
	GetUpdatesChan(config tgbotapi.UpdateConfig) tgbotapi.UpdatesChannel
	GetSelf() tgbotapi.User
	StopReceivingUpdates()
}

type SheetsWriter interface {
	UpdateUsersSheet(ctx context.Context, users []*models.User) error
	UpdateBookingsSheet(ctx context.Context, bookings []*models.Booking) error
	ReplaceBookingsSheet(ctx context.Context, bookings []*models.Booking) error
	AppendBooking(ctx context.Context, booking *models.Booking) error
	UpdateScheduleSheet(
		ctx context.Context,
		startDate, endDate time.Time,
		dailyBookings map[string][]*models.Booking,
		items []*models.Item,
	) error
	UpsertBooking(ctx context.Context, booking *models.Booking) error
	UpdateBookingStatus(ctx context.Context, bookingID int64, status string) error
}

type SyncWorker interface {
	EnqueueTask(ctx context.Context, taskType string, bookingID int64, booking *models.Booking, status string) error
	EnqueueSyncSchedule(ctx context.Context, startDate, endDate time.Time) error
}

type TelegramService interface {
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
	Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error)
	SendMessage(chatID int64, text string) (tgbotapi.Message, error)
	SendMarkdown(chatID int64, text string) (tgbotapi.Message, error)
	SendWithKeyboard(chatID int64, text string, keyboard tgbotapi.ReplyKeyboardMarkup) (tgbotapi.Message, error)
	SendWithInlineKeyboard(chatID int64, text string, keyboard tgbotapi.InlineKeyboardMarkup) (tgbotapi.Message, error)
	EditMessage(chatID int64, messageID int, text string, keyboard *tgbotapi.InlineKeyboardMarkup) (tgbotapi.Message, error)
	AnswerCallback(callbackID string, text string) error
	GetUpdatesChan(config tgbotapi.UpdateConfig) tgbotapi.UpdatesChannel
	GetSelf() tgbotapi.User
	StopReceivingUpdates()
}

type BookingService interface {
	ValidateBookingDate(date time.Time) error
	CreateBooking(ctx context.Context, booking *models.Booking) error
	ConfirmBooking(ctx context.Context, bookingID int64, version int64, managerID int64) error
	RejectBooking(ctx context.Context, bookingID int64, version int64, managerID int64) error
	CompleteBooking(ctx context.Context, bookingID int64, version int64, managerID int64) error
	ReopenBooking(ctx context.Context, bookingID int64, version int64, managerID int64) error
	ChangeBookingItem(ctx context.Context, bookingID int64, version int64, newItemID int64, managerID int64) error
	RescheduleBooking(ctx context.Context, bookingID int64, managerID int64) error
	GetAvailability(ctx context.Context, itemID int64, startDate time.Time, days int) ([]*models.Availability, error)
	CheckAvailability(ctx context.Context, itemID int64, date time.Time) (bool, error)
	GetBookedCount(ctx context.Context, itemID int64, date time.Time) (int, error)
	GetBookingsByDateRange(ctx context.Context, start, end time.Time) ([]*models.Booking, error)
	GetBooking(ctx context.Context, id int64) (*models.Booking, error)
	GetDailyBookings(ctx context.Context, start, end time.Time) (map[string][]*models.Booking, error)
}

type UserService interface {
	IsManager(userID int64) bool
	IsBlacklisted(userID int64) bool
	SaveUser(ctx context.Context, user *models.User) error
	UpdateUserPhone(ctx context.Context, telegramID int64, phone string) error
	UpdateUserActivity(ctx context.Context, telegramID int64) error
	GetAllUsers(ctx context.Context) ([]*models.User, error)
	GetActiveUsers(ctx context.Context, days int) ([]*models.User, error)
	GetManagers(ctx context.Context) ([]*models.User, error)
	GetUserBookings(ctx context.Context, userID int64) ([]*models.Booking, error)
	GetUserByID(ctx context.Context, id int64) (*models.User, error)
}

type ItemService interface {
	GetActiveItems(ctx context.Context) ([]*models.Item, error)
	GetItemByID(ctx context.Context, id int64) (*models.Item, error)
	GetItemByName(ctx context.Context, name string) (*models.Item, error)
	CreateItem(ctx context.Context, item *models.Item) error
	UpdateItem(ctx context.Context, item *models.Item) error
	DeactivateItem(ctx context.Context, id int64) error
	ReorderItem(ctx context.Context, id int64, newOrder int64) error
}
