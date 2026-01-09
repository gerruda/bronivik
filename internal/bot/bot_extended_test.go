package bot

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"bronivik/internal/models"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/xuri/excelize/v2"
)

func TestHandleManagerCommand(t *testing.T) {
	b, mocks := setupTestBot()
	ctx := context.Background()
	managerID := int64(123)

	// Add a booking so "All Bookings" works
	mocks.booking.setBookings(map[int64]*models.Booking{
		1: {ID: 1, UserID: 1, ItemName: "Item 1", Date: time.Now(), Status: models.StatusPending},
	})

	tests := []struct {
		name     string
		text     string
		expected string
	}{
		{"All Bookings Button", "👨‍💼 Все заявки", "Все заявки на квартал вперед"},
		{"All Bookings Cmd", "/get_all", "Все заявки на квартал вперед"},
		{"Create Booking Button", "➕ Создать заявку (Менеджер)", "Введите Имя клиента"},
		{"Stats Cmd", "/stats", "Статистика"},
		{"Sync Bookings", "🔄 Синхронизировать бронирования (Google Sheets)", "Запускаю фоновую синхронизацию"},
		{"Sync Schedule", "📅 Синхронизировать расписание (Google Sheets)", "Запускаю фоновую синхронизацию"},
		{"Add Item Prompt", "/add_item", "/add_item <название> <количество>"},
		{"List Items Prompt", "/list_items", "Список активных аппаратов"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mocks.tg.clearSentMessages()
			err := mocks.state.ClearUserState(ctx, managerID) // Clear state each time
			assert.NoError(t, err)

			update := tgbotapi.Update{
				Message: &tgbotapi.Message{
					From: &tgbotapi.User{ID: managerID},
					Chat: &tgbotapi.Chat{ID: managerID},
					Text: tt.text,
				},
			}
			b.handleManagerCommand(ctx, &update)

			msgs := mocks.tg.getSentMessages()
			assert.NotEmpty(t, msgs, "Expected message for %s", tt.text)
			found := false
			for _, m := range msgs {
				if msg, ok := m.(tgbotapi.MessageConfig); ok {
					if strings.Contains(msg.Text, tt.expected) {
						found = true
						break
					}
				}
			}
			assert.True(t, found, "Expected to find '%s' in messages for '%s', got: %v", tt.expected, tt.text, msgs)
		})
	}
}

func TestHandleManagerCallback(t *testing.T) {
	b, mocks := setupTestBot()
	ctx := context.Background()
	managerID := int64(123)

	// Setup a booking
	booking := &models.Booking{ID: 1, UserID: 456, Status: models.StatusPending, Date: time.Now()}
	mocks.booking.setBookings(map[int64]*models.Booking{1: booking})

	tests := []struct {
		name   string
		data   string
		status string
	}{
		{"Confirm", "confirm_1", models.StatusConfirmed},
		{"Reject", "reject_1", models.StatusCanceled},
		{"Complete", "complete_1", models.StatusCompleted},
		{"Reopen", "reopen_1", models.StatusPending},
		{"Details", "details_1", ""},
		{"Reschedule", "reschedule_1", models.StatusChanged},
		{"Change Item", "change_item_1", ""},
		{"Edit Comment", "edit_comment_1", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			update := tgbotapi.Update{
				CallbackQuery: &tgbotapi.CallbackQuery{
					ID:   "cb",
					From: &tgbotapi.User{ID: managerID},
					Message: &tgbotapi.Message{
						Chat:      &tgbotapi.Chat{ID: managerID},
						MessageID: 1,
					},
					Data: tt.data,
				},
			}
			b.handleManagerCallback(ctx, &update)
			if tt.status != "" {
				assert.Equal(t, tt.status, booking.Status)
			}
		})
	}
}

func TestGetCellStyle(t *testing.T) {
	b := &Bot{}
	f := excelize.NewFile()

	// 1. No active bookings -> White style
	style1, err := b.getCellStyle(f, []*models.Booking{}, 0, 1)
	assert.NoError(t, err)

	// 2. Fully booked -> Red style
	style2, err := b.getCellStyle(f, []*models.Booking{{Status: models.StatusConfirmed}}, 1, 1)
	assert.NoError(t, err)

	// 3. Has unconfirmed -> Yellow style
	style3, err := b.getCellStyle(f, []*models.Booking{{Status: models.StatusPending}}, 0, 1)
	assert.NoError(t, err)

	// 4. All confirmed but not full -> Green style
	style4, err := b.getCellStyle(f, []*models.Booking{{Status: models.StatusConfirmed}}, 1, 5)
	assert.NoError(t, err)

	assert.NotEqual(t, style1, style2)
	assert.NotEqual(t, style1, style3)
	assert.NotEqual(t, style1, style4)
}

func TestExportToExcel_Styling(t *testing.T) {
	b, mocks := setupTestBot()
	ctx := context.Background()

	tmpDir, _ := os.MkdirTemp("", "bronivik_style")
	defer os.RemoveAll(tmpDir)
	b.config.Exports.Path = tmpDir

	now := time.Now()
	// Mock bookings with various statuses to trigger getCellStyle
	mocks.booking.setBookings(map[int64]*models.Booking{
		1: {ID: 1, ItemID: 1, Date: now, Status: models.StatusConfirmed, UserName: "User 1"},
		2: {ID: 2, ItemID: 1, Date: now.Add(24 * time.Hour), Status: models.StatusPending, UserName: "User 2"},
		3: {ID: 3, ItemID: 1, Date: now.Add(48 * time.Hour), Status: models.StatusCompleted, UserName: "User 3"},
	})

	path, err := b.exportToExcel(ctx, now, now.Add(7*24*time.Hour))
	assert.NoError(t, err)
	assert.NotEmpty(t, path)
}
func TestUtils_More(t *testing.T) {
	b, mocks := setupTestBot()
	ctx := context.Background()

	t.Run("parseDate", func(t *testing.T) {
		d1 := parseDate("2023-01-01")
		assert.Equal(t, 2023, d1.Year())
		d2 := parseDate("invalid")
		assert.True(t, d2.IsZero())
	})

	t.Run("getLastColumn", func(t *testing.T) {
		assert.Equal(t, "A", getLastColumn(1))
		assert.Equal(t, "Z", getLastColumn(26))
		assert.Equal(t, "AA", getLastColumn(27))
		assert.Equal(t, "AZ", getLastColumn(52))
	})

	t.Run("requestSpecificDate", func(t *testing.T) {
		update := tgbotapi.Update{Message: &tgbotapi.Message{From: &tgbotapi.User{ID: 123}, Chat: &tgbotapi.Chat{ID: 123}}}
		b.requestSpecificDate(ctx, &update)
		assert.NotEmpty(t, mocks.tg.getSentMessages())
	})

	t.Run("editManagerItemsPage", func(t *testing.T) {
		update := tgbotapi.Update{
			CallbackQuery: &tgbotapi.CallbackQuery{
				From: &tgbotapi.User{ID: 123},
				Message: &tgbotapi.Message{
					Chat:      &tgbotapi.Chat{ID: 123},
					MessageID: 1,
				},
			},
		}
		b.editManagerItemsPage(&update, 0)
		// Should not panic
	})
}

func TestBot_HandleCallButton(t *testing.T) {
	b, mocks := setupTestBot()
	booking := &models.Booking{ID: 123, UserName: "Tester", Phone: "+79001112233", ItemName: "Camera", Date: time.Now()}
	mocks.booking.setBookings(map[int64]*models.Booking{123: booking})

	update := tgbotapi.Update{
		CallbackQuery: &tgbotapi.CallbackQuery{
			Data: "call_booking:123",
			ID:   "cb123",
			From: &tgbotapi.User{ID: 1},
			Message: &tgbotapi.Message{
				Chat: &tgbotapi.Chat{ID: 100},
			},
		},
	}

	b.handleCallButton(context.Background(), &update)
	assert.NotEmpty(t, mocks.tg.getSentMessages())
}

func TestBot_ShowUserBookings(t *testing.T) {
	b, mocks := setupTestBot()
	booking := &models.Booking{ID: 1, UserID: 1, ItemName: "Item1", Status: "confirmed", Date: time.Now()}
	mocks.user.setBookings([]*models.Booking{booking})

	update := tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{ID: 1},
			Chat: &tgbotapi.Chat{ID: 100},
		},
	}

	b.showUserBookings(context.Background(), &update)
	assert.NotEmpty(t, mocks.tg.getSentMessages())
}

func TestBot_BookingSummary(t *testing.T) {
	b, mocks := setupTestBot()
	now := time.Now()
	bookings := []*models.Booking{
		{ID: 1, Status: models.StatusConfirmed, ItemName: "Item1", Date: now},
		{ID: 2, Status: models.StatusPending, ItemName: "Item1", Date: now},
		{ID: 3, Status: models.StatusConfirmed, ItemName: "Item2", Date: now},
	}
	mocks.booking.On("GetBookingsByDateRange", mock.Anything, mock.Anything, mock.Anything).Return(bookings, nil).Maybe()

	res := b.bookingSummary(context.Background(), now.Add(-time.Hour), now.Add(time.Hour))
	assert.Contains(t, res, "всего 3")
}

func (m *mockUserService) setBookings(bookings []*models.Booking) {
	m.On("GetUserBookings", mock.Anything, mock.Anything).Return(bookings, nil).Maybe()
}

func TestBot_FormattingUtils_Extended(t *testing.T) {
	b, _ := setupTestBot()

	t.Run("FormatPhone", func(t *testing.T) {
		res := b.formatPhoneForDisplay("+79001112233")
		assert.Equal(t, "+7 (900) 111-22-33", res)
		res2 := b.formatPhoneForDisplay("invalid")
		assert.Equal(t, "invalid", res2)
	})
}
