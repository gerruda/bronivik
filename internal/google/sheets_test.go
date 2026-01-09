package google

import (
	"bronivik/internal/models"
	"context"
	"os"
	"testing"
	"time"
)

func TestFilterActiveBookings(t *testing.T) {
	s := &SheetsService{}

	bookings := []*models.Booking{
		{ID: 1, Status: "pending"},
		{ID: 2, Status: "confirmed"},
		{ID: 3, Status: "canceled"},
		{ID: 4, Status: "completed"},
	}

	active := s.filterActiveBookings(bookings)

	if len(active) != 3 {
		t.Errorf("Expected 3 active bookings, got %d", len(active))
	}

	for _, b := range active {
		if b.Status == "canceled" {
			t.Errorf("Canceled booking found in active list")
		}
	}
}

func TestBookingRowValues(t *testing.T) {
	date := time.Date(2024, 12, 25, 0, 0, 0, 0, time.UTC)
	createdAt := time.Date(2024, 12, 20, 10, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2024, 12, 21, 11, 0, 0, 0, time.UTC)

	booking := &models.Booking{
		ID:        123,
		UserID:    456,
		ItemID:    789,
		Date:      date,
		Status:    "confirmed",
		UserName:  "Test User",
		Phone:     "79991234567",
		ItemName:  "Test Item",
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}

	values := bookingRowValues(booking)

	expected := []interface{}{
		int64(123),
		int64(456),
		int64(789),
		"2024-12-25",
		"confirmed",
		"Test User",
		"79991234567",
		"Test Item",
		"2024-12-20 10:00:00",
		"2024-12-21 11:00:00",
	}

	if len(values) != len(expected) {
		t.Fatalf("Expected %d values, got %d", len(expected), len(values))
	}

	for i, v := range values {
		if v != expected[i] {
			t.Errorf("At index %d: expected %v, got %v", i, expected[i], v)
		}
	}
}

func TestCacheOperations(t *testing.T) {
	s := &SheetsService{
		rowCache: make(map[int64]int),
	}

	s.setCachedRow(100, 5)
	row, ok := s.getCachedRow(100)
	if !ok || row != 5 {
		t.Errorf("Expected row 5, got %d (ok=%v)", row, ok)
	}

	s.deleteCacheRow(100)
	_, ok = s.getCachedRow(100)
	if ok {
		t.Errorf("Expected row to be deleted from cache")
	}

	s.setCachedRow(200, 10)
	s.ClearCache()
	_, ok = s.getCachedRow(200)
	if ok {
		t.Errorf("Expected cache to be cleared")
	}
}

func TestPrepareDateHeaders(t *testing.T) {
	s := &SheetsService{}
	startDate := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC)

	headers, cols := s.prepareDateHeaders(startDate, endDate)
	if cols != 3 {
		t.Errorf("Expected 3 columns, got %d", cols)
	}
	if len(headers) != 4 {
		t.Errorf("Expected 4 headers, got %d", len(headers))
	}
	if headers[1] != "01.01" || headers[2] != "02.01" || headers[3] != "03.01" {
		t.Errorf("Unexpected headers: %v", headers)
	}
}

func TestFormatScheduleCell(t *testing.T) {
	s := &SheetsService{}
	item := &models.Item{Name: "Camera", TotalQuantity: 2}

	t.Run("Empty", func(t *testing.T) {
		val, color := s.formatScheduleCell(item, nil)
		if val == "" || color == nil {
			t.Error("Expected non-empty value and color")
		}
	})

	t.Run("Booked", func(t *testing.T) {
		bookings := []*models.Booking{
			{ID: 1, UserName: "User 1", Phone: "111", Status: models.StatusConfirmed},
		}
		val, color := s.formatScheduleCell(item, bookings)
		if val == "" {
			t.Error("Expected non-empty value")
		}
		// Green-ish
		if color.Green < 0.9 {
			t.Errorf("Expected green color, got %+v", color)
		}
	})

	t.Run("FullyBooked", func(t *testing.T) {
		bookings := []*models.Booking{
			{ID: 1, UserName: "User 1", Phone: "111", Status: models.StatusConfirmed},
			{ID: 2, UserName: "User 2", Phone: "222", Status: models.StatusConfirmed},
		}
		val, color := s.formatScheduleCell(item, bookings)
		if val == "" {
			t.Error("Expected non-empty value")
		}
		// Red-ish
		if color.Red < 0.9 {
			t.Errorf("Expected red color, got %+v", color)
		}
	})

	t.Run("Unconfirmed", func(t *testing.T) {
		bookings := []*models.Booking{
			{ID: 1, UserName: "User 1", Phone: "111", Status: models.StatusPending},
		}
		val, color := s.formatScheduleCell(item, bookings)
		if val == "" {
			t.Error("Expected non-empty value")
		}
		// Yellow-ish
		if color.Red < 0.9 || color.Green < 0.9 {
			t.Errorf("Expected yellow color, got %+v", color)
		}
	})
}

func TestPrepareItemRowData(t *testing.T) {
	s := &SheetsService{}
	item := &models.Item{ID: 1, Name: "Camera", TotalQuantity: 2}
	startDate := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	dailyBookings := map[string][]*models.Booking{
		"2025-01-01": {{ID: 1, ItemID: 1, Status: models.StatusConfirmed}},
	}

	rowData, cellFormats := s.prepareItemRowData(item, startDate, 2, dailyBookings)
	if len(rowData) != 3 {
		t.Errorf("Expected 3 elements in rowData, got %d", len(rowData))
	}
	if len(cellFormats) != 2 {
		t.Errorf("Expected 2 cellFormats, got %d", len(cellFormats))
	}
}

func TestPrepareEmptyItemsRow(t *testing.T) {
	s := &SheetsService{}
	row := s.prepareEmptyItemsRow(3)
	if len(row) != 4 {
		t.Errorf("Expected 4 elements, got %d", len(row))
	}
	if row[0] != "Нет доступных аппаратов" {
		t.Errorf("Unexpected first element: %v", row[0])
	}
}

func TestGetServiceAccountEmail(t *testing.T) {
	s := &SheetsService{}
	content := `{"client_email": "test@example.com"}`
	tmpfile, err := os.CreateTemp("", "creds.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err = tmpfile.WriteString(content); err != nil {
		t.Fatal(err)
	}
	if err = tmpfile.Close(); err != nil {
		t.Fatal(err)
	}

	email, err := s.GetServiceAccountEmail(tmpfile.Name())
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if email != "test@example.com" {
		t.Errorf("Expected test@example.com, got %s", email)
	}

	_, err = s.GetServiceAccountEmail("non-existent")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
}

func TestGetPeriodHeaderFormat(t *testing.T) {
	s := &SheetsService{}
	req := s.getPeriodHeaderFormat(123)
	if req == nil || req.RepeatCell == nil {
		t.Error("Expected RepeatCell request")
	}
	if req.RepeatCell.Range.SheetId != 123 {
		t.Errorf("Expected sheet ID 123, got %d", req.RepeatCell.Range.SheetId)
	}
}

func TestGetDateHeadersFormat(t *testing.T) {
	s := &SheetsService{}
	req := s.getDateHeadersFormat(456, 5)
	if req == nil || req.RepeatCell == nil {
		t.Error("Expected RepeatCell request")
	}
	if req.RepeatCell.Range.SheetId != 456 {
		t.Errorf("Expected sheet ID 456, got %d", req.RepeatCell.Range.SheetId)
	}
	if req.RepeatCell.Range.EndColumnIndex != 5 {
		t.Errorf("Expected end column 5, got %d", req.RepeatCell.Range.EndColumnIndex)
	}
}

func TestGetItemNamesFormat(t *testing.T) {
	s := &SheetsService{}
	req := s.getItemNamesFormat(789, 3)
	if req == nil || req.RepeatCell == nil {
		t.Error("Expected RepeatCell request")
	}
	if req.RepeatCell.Range.SheetId != 789 {
		t.Errorf("Expected sheet ID 789, got %d", req.RepeatCell.Range.SheetId)
	}
	if req.RepeatCell.Range.EndRowIndex != 6 { // 3 + 3
		t.Errorf("Expected end row 6, got %d", req.RepeatCell.Range.EndRowIndex)
	}
}

func TestFindBookingRow(t *testing.T) {
	s := &SheetsService{
		rowCache: make(map[int64]int),
	}

	t.Run("ZeroID", func(t *testing.T) {
		_, err := s.FindBookingRow(context.Background(), 0)
		if err == nil {
			t.Error("Expected error for zero ID")
		}
	})

	t.Run("CachedRow", func(t *testing.T) {
		s.setCachedRow(123, 5)
		row, err := s.FindBookingRow(context.Background(), 123)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if row != 5 {
			t.Errorf("Expected row 5, got %d", row)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		// Skip this test as it requires real Google Sheets API
		t.Skip("Requires real Google Sheets service")
	})
}

func TestUpsertBooking(t *testing.T) {
	s := &SheetsService{
		rowCache: make(map[int64]int),
	}

	t.Run("NilBooking", func(t *testing.T) {
		err := s.UpsertBooking(context.Background(), nil)
		if err == nil {
			t.Error("Expected error for nil booking")
		}
	})

	t.Run("NewBooking", func(t *testing.T) {
		// Skip this test as it requires real Google Sheets API
		t.Skip("Requires real Google Sheets service")
	})
}

func TestDeleteBookingRow(t *testing.T) {
	t.Run("NotFound", func(t *testing.T) {
		// Skip this test as it requires real Google Sheets API
		t.Skip("Requires real Google Sheets service")
	})
}

func TestUpdateBookingStatus(t *testing.T) {
	t.Run("NotFound", func(t *testing.T) {
		// Skip this test as it requires real Google Sheets API
		t.Skip("Requires real Google Sheets service")
	})
}

func TestUpdateUsersSheet(t *testing.T) {
	// Skip this test as it requires real Google Sheets API
	t.Skip("Requires real Google Sheets service")
}

func TestUpdateBookingsSheet(t *testing.T) {
	// Skip this test as it requires real Google Sheets API
	t.Skip("Requires real Google Sheets service")
}

func TestReplaceBookingsSheet(t *testing.T) {
	// Skip this test as it requires real Google Sheets API
	t.Skip("Requires real Google Sheets service")
}

func TestUpdateScheduleSheet(t *testing.T) {
	t.Run("InvalidDateRange", func(t *testing.T) {
		// Skip this test as it requires real Google Sheets API
		t.Skip("Requires real Google Sheets service")
	})

	t.Run("ValidCall", func(t *testing.T) {
		// Skip this test as it requires real Google Sheets API
		t.Skip("Requires real Google Sheets service")
	})
}

func TestGetSheetIdByName(t *testing.T) {
	// Skip this test as it requires real Google Sheets API
	t.Skip("Requires real Google Sheets service")
}

func TestNewSimpleSheetsService(t *testing.T) {
	// Skip this test as it requires real Google credentials
	t.Skip("Requires real Google credentials")
}

func TestTestConnection(t *testing.T) {
	// Skip this test as it requires real Google Sheets API
	t.Skip("Requires real Google Sheets service")
}

func TestWarmUpCache(t *testing.T) {
	// Skip this test as it requires real Google Sheets API
	t.Skip("Requires real Google Sheets service")
}

func TestAppendBooking(t *testing.T) {
	// Skip this test as it requires real Google Sheets API
	t.Skip("Requires real Google Sheets service")
}
