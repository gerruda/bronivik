package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"bronivik/internal/config"
	"bronivik/internal/database"
	"bronivik/internal/models"

	"github.com/rs/zerolog"
)

// Integration-style test: HTTP availability reflects bookings persisted in DB.
func TestAvailabilityReflectsBookings(t *testing.T) {
	db := newIntegrationDB(t)
	item := createIntegrationItem(t, db, "camera", 1)
	db.SetItems([]*models.Item{&item})

	server := newIntegrationHTTPServer(db)
	ts := httptest.NewServer(server.server.Handler)
	t.Cleanup(ts.Close)

	date := time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)
	dateStr := date.Format("2006-01-02")

	checkAvailable(t, ts.URL, item.Name, dateStr, true, 0, item.TotalQuantity)

	insertIntegrationBooking(t, db, &item, date, "pending")

	checkAvailable(t, ts.URL, item.Name, dateStr, false, 1, item.TotalQuantity)
}

func newIntegrationHTTPServer(db *database.DB) *HTTPServer {
	cfg := config.APIConfig{Enabled: true, HTTP: config.APIHTTPConfig{Enabled: true, Port: 0}, Auth: config.APIAuthConfig{Enabled: false}}
	logger := zerolog.New(io.Discard)
	return NewHTTPServer(&cfg, db, nil, nil, &logger)
}

func newIntegrationDB(t *testing.T) *database.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "integration.db")
	logger := zerolog.New(io.Discard)
	db, err := database.NewDB(path, &logger)
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func createIntegrationItem(t *testing.T, db *database.DB, name string, total int64) models.Item {
	t.Helper()
	item := models.Item{Name: name, TotalQuantity: total, SortOrder: 1}
	if err := db.CreateItem(context.Background(), &item); err != nil {
		t.Fatalf("create item: %v", err)
	}
	return item
}

func insertIntegrationBooking(t *testing.T, db *database.DB, item *models.Item, date time.Time, status string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO bookings (
			user_id, user_name, user_nickname, phone, 
			item_id, item_name, date, status, comment, 
			created_at, updated_at, version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 1)
	`, int64(1), "tester", "tester_nick", "+100", item.ID, item.Name, date.Format("2006-01-02"), status)
	if err != nil {
		t.Fatalf("insert booking: %v", err)
	}
}

func checkAvailable(t *testing.T, baseURL, itemName, dateStr string, wantAvailable bool, wantBooked, wantTotal int64) {
	t.Helper()
	url := fmt.Sprintf("%s/api/v1/availability/%s?date=%s", baseURL, itemName, dateStr)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d", resp.StatusCode)
	}

	var body struct {
		Available   bool  `json:"available"`
		BookedCount int64 `json:"booked_count"`
		Total       int64 `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if body.Available != wantAvailable {
		t.Fatalf("available: want %v got %v", wantAvailable, body.Available)
	}
	if body.BookedCount != wantBooked {
		t.Fatalf("booked_count: want %d got %d", wantBooked, body.BookedCount)
	}
	if body.Total != wantTotal {
		t.Fatalf("total: want %d got %d", wantTotal, body.Total)
	}
}
