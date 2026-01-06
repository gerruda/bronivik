package google

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"bronivik/internal/models"

	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

func setupMockServer(ctx context.Context) (*http.ServeMux, *httptest.Server, *SheetsService) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	srv, _ := sheets.NewService(ctx, option.WithEndpoint(server.URL), option.WithoutAuthentication())
	s := &SheetsService{
		service:         srv,
		usersSheetID:    "users_tid",
		bookingsSheetID: "bookings_tid",
		rowCache:        make(map[int64]int),
	}
	return mux, server, s
}

func TestSheetsService_TestConnection(t *testing.T) {
	ctx := context.Background()
	mux, server, s := setupMockServer(ctx)
	defer server.Close()
	mux.HandleFunc("/v4/spreadsheets/users_tid/values/Users!A1", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(sheets.ValueRange{Values: [][]interface{}{{"test"}}})
	})
	err := s.TestConnection(ctx)
	if err != nil {
		t.Errorf("TestConnection failed: %v", err)
	}
}

func TestSheetsService_UpdateUsersSheet(t *testing.T) {
	ctx := context.Background()
	mux, server, s := setupMockServer(ctx)
	defer server.Close()
	mux.HandleFunc("/v4/spreadsheets/users_tid/values/Users!A1:K2", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(sheets.UpdateValuesResponse{})
	})
	users := []*models.User{{ID: 1, Username: "test", CreatedAt: time.Now(), LastActivity: time.Now()}}
	err := s.UpdateUsersSheet(ctx, users)
	if err != nil {
		t.Errorf("UpdateUsersSheet failed: %v", err)
	}
}

func TestSheetsService_WarmUpCache(t *testing.T) {
	ctx := context.Background()
	mux, server, s := setupMockServer(ctx)
	defer server.Close()
	mux.HandleFunc("/v4/spreadsheets/bookings_tid/values/Bookings!A:A", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(sheets.ValueRange{
			Values: [][]interface{}{{"ID"}, {"123"}, {"456"}},
		})
	})
	err := s.WarmUpCache(ctx)
	if err != nil {
		t.Errorf("WarmUpCache failed: %v", err)
	}
	if row, ok := s.getCachedRow(123); !ok || row != 2 {
		t.Errorf("Expected row 2 for ID 123, got %d", row)
	}
}

func TestSheetsService_AppendBooking(t *testing.T) {
	ctx := context.Background()
	mux, server, s := setupMockServer(ctx)
	defer server.Close()
	mux.HandleFunc("/v4/spreadsheets/bookings_tid/values/Bookings!A:A:append", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(sheets.AppendValuesResponse{
			Updates: &sheets.UpdateValuesResponse{
				UpdatedRange: "Bookings!A10:J10",
			},
		})
	})
	booking := &models.Booking{ID: 789, Date: time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	err := s.AppendBooking(ctx, booking)
	if err != nil {
		t.Errorf("AppendBooking failed: %v", err)
	}
	if row, _ := s.getCachedRow(789); row != 10 {
		t.Errorf("Expected cached row 10, got %d", row)
	}
}

func TestSheetsService_UpsertBooking_Update(t *testing.T) {
	ctx := context.Background()
	mux, server, s := setupMockServer(ctx)
	defer server.Close()
	s.setCachedRow(123, 2)
	mux.HandleFunc("/v4/spreadsheets/bookings_tid/values/Bookings!A2:J2", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(sheets.UpdateValuesResponse{})
	})
	booking := &models.Booking{ID: 123, Date: time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	err := s.UpsertBooking(ctx, booking)
	if err != nil {
		t.Errorf("UpsertBooking failed: %v", err)
	}
}

func TestSheetsService_DeleteBookingRow(t *testing.T) {
	ctx := context.Background()
	mux, server, s := setupMockServer(ctx)
	defer server.Close()
	s.setCachedRow(456, 3)
	mux.HandleFunc("/v4/spreadsheets/bookings_tid/values/Bookings!A3:J3:clear", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(sheets.ClearValuesResponse{})
	})
	err := s.DeleteBookingRow(ctx, 456)
	if err != nil {
		t.Errorf("DeleteBookingRow failed: %v", err)
	}
	if _, ok := s.getCachedRow(456); ok {
		t.Error("Expected 456 to be removed from cache")
	}
}

func TestSheetsService_UpdateBookingStatus(t *testing.T) {
	ctx := context.Background()
	mux, server, s := setupMockServer(ctx)
	defer server.Close()
	s.setCachedRow(123, 2)
	mux.HandleFunc("/v4/spreadsheets/bookings_tid/values/Bookings!E2:E2", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(sheets.UpdateValuesResponse{})
	})
	mux.HandleFunc("/v4/spreadsheets/bookings_tid/values/Bookings!J2:J2", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(sheets.UpdateValuesResponse{})
	})
	err := s.UpdateBookingStatus(ctx, 123, "confirmed")
	if err != nil {
		t.Errorf("UpdateBookingStatus failed: %v", err)
	}
}

func TestSheetsService_GetSheetIdByName(t *testing.T) {
	ctx := context.Background()
	mux, server, s := setupMockServer(ctx)
	defer server.Close()
	mux.HandleFunc("/v4/spreadsheets/bookings_tid", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(sheets.Spreadsheet{
			Sheets: []*sheets.Sheet{
				{
					Properties: &sheets.SheetProperties{
						Title:   "Бронирования",
						SheetId: 999,
					},
				},
			},
		})
	})
	id, err := s.GetSheetIdByName(ctx, s.bookingsSheetID, "Бронирования")
	if err != nil {
		t.Errorf("GetSheetIdByName failed: %v", err)
	}
	if id != 999 {
		t.Errorf("Expected 999, got %d", id)
	}
}

func TestSheetsService_UpdateBookingsSheet(t *testing.T) {
	ctx := context.Background()
	mux, server, s := setupMockServer(ctx)
	defer server.Close()
	mux.HandleFunc("/v4/spreadsheets/bookings_tid/values/Bookings!A1:J2", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(sheets.UpdateValuesResponse{})
	})
	bookings := []*models.Booking{{ID: 1, UserName: "test", Date: time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now()}}
	err := s.UpdateBookingsSheet(ctx, bookings)
	if err != nil {
		t.Errorf("UpdateBookingsSheet failed: %v", err)
	}
}

func TestSheetsService_ReplaceBookingsSheet(t *testing.T) {
	ctx := context.Background()
	mux, server, s := setupMockServer(ctx)
	defer server.Close()
	mux.HandleFunc("/v4/spreadsheets/bookings_tid/values/Bookings!A2:Z:clear", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(sheets.ClearValuesResponse{})
	})
	mux.HandleFunc("/v4/spreadsheets/bookings_tid/values/Bookings!A2", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(sheets.UpdateValuesResponse{})
	})
	bookings := []*models.Booking{{ID: 1, UserName: "test", Date: time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now()}}
	err := s.ReplaceBookingsSheet(ctx, bookings)
	if err != nil {
		t.Errorf("ReplaceBookingsSheet failed: %v", err)
	}
	if row, _ := s.getCachedRow(1); row != 2 {
		t.Errorf("Expected cached row 2, got %d", row)
	}
}

func TestSheetsService_UpdateScheduleSheet(t *testing.T) {
	ctx := context.Background()
	mux, server, s := setupMockServer(ctx)
	defer server.Close()
	mux.HandleFunc("/v4/spreadsheets/bookings_tid", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(sheets.Spreadsheet{
			Sheets: []*sheets.Sheet{
				{
					Properties: &sheets.SheetProperties{
						Title:   "Бронирования",
						SheetId: 999,
					},
				},
			},
		})
	})
	mux.HandleFunc("/v4/spreadsheets/bookings_tid/values/Бронирования!A:Z:clear", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(sheets.ClearValuesResponse{})
	})
	mux.HandleFunc("/v4/spreadsheets/bookings_tid/values/Бронирования!A1", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(sheets.UpdateValuesResponse{})
	})
	mux.HandleFunc("/v4/spreadsheets/bookings_tid:batchUpdate", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(sheets.BatchUpdateSpreadsheetResponse{})
	})

	startDate := time.Now()
	endDate := startDate.AddDate(0, 0, 1)
	dailyBookings := map[string][]*models.Booking{
		startDate.Format("2006-01-02"): {{ID: 1, ItemID: 1, Status: "confirmed"}},
	}
	items := []*models.Item{{ID: 1, Name: "Item 1", TotalQuantity: 5}}

	err := s.UpdateScheduleSheet(ctx, startDate, endDate, dailyBookings, items)
	if err != nil {
		t.Errorf("UpdateScheduleSheet failed: %v", err)
	}
}

func TestSheetsService_FindBookingRow_FullScan(t *testing.T) {
	ctx := context.Background()
	mux, server, s := setupMockServer(ctx)
	defer server.Close()
	mux.HandleFunc("/v4/spreadsheets/bookings_tid/values/Bookings!A:A", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(sheets.ValueRange{
			Values: [][]interface{}{{"ID"}, {"999"}},
		})
	})
	row, err := s.FindBookingRow(ctx, 999)
	if err != nil {
		t.Errorf("FindBookingRow failed: %v", err)
	}
	if row != 2 {
		t.Errorf("Expected row 2, got %d", row)
	}
}
