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

func TestAvailabilitySuccess(t *testing.T) {
	db := newTestDB(t)
	item := createTestItem(t, db, "camera", 2)
	bookingDate := time.Date(2025, 12, 1, 10, 0, 0, 0, time.UTC)
	insertTestBooking(t, db, item, bookingDate, "pending")

	server := newTestHTTPServer(db)
	ts := httptest.NewServer(server.server.Handler)
	t.Cleanup(ts.Close)

	url := fmt.Sprintf("%s/api/v1/availability/%s?date=%s", ts.URL, item.Name, bookingDate.Format("2006-01-02"))
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}

	var body struct {
		Available   bool  `json:"available"`
		BookedCount int64 `json:"booked_count"`
		Total       int64 `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if !body.Available {
		t.Fatalf("expected available=true, got false")
	}
	if body.BookedCount != 1 {
		t.Fatalf("expected booked_count=1, got %d", body.BookedCount)
	}
	if body.Total != item.TotalQuantity {
		t.Fatalf("expected total=%d, got %d", item.TotalQuantity, body.Total)
	}
}

func TestAvailabilityNotFound(t *testing.T) {
	db := newTestDB(t)
	server := newTestHTTPServer(db)
	ts := httptest.NewServer(server.server.Handler)
	t.Cleanup(ts.Close)

	url := fmt.Sprintf("%s/api/v1/availability/%s?date=%s", ts.URL, "unknown", "2025-12-01")
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func newTestHTTPServer(db *database.DB) *HTTPServer {
	cfg := config.APIConfig{
		Enabled: true,
		HTTP:    config.APIHTTPConfig{Enabled: true, Port: 0},
		Auth:    config.APIAuthConfig{Enabled: false},
	}
	logger := zerolog.New(io.Discard)
	return NewHTTPServer(cfg, db, &logger)
}

func newTestDB(t *testing.T) *database.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	logger := zerolog.New(io.Discard)
	db, err := database.NewDB(path, &logger)
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func createTestItem(t *testing.T, db *database.DB, name string, total int64) models.Item {
	t.Helper()
	item := models.Item{Name: name, TotalQuantity: total, SortOrder: 1}
	if err := db.CreateItem(context.Background(), &item); err != nil {
		t.Fatalf("create item: %v", err)
	}
	return item
}

func insertTestBooking(t *testing.T, db *database.DB, item models.Item, date time.Time, status string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO bookings (user_id, user_name, user_nickname, phone, item_id, item_name, date, status, comment, created_at, updated_at, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 1)
	`,
		int64(1),
		"tester",
		"tester_nick",
		"+100000000",
		item.ID,
		item.Name,
		date.Format("2006-01-02"),
		status,
	)
	if err != nil {
		t.Fatalf("insert booking: %v", err)
	}
}
