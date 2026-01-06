package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	availabilityv1 "bronivik/internal/api/gen/availability/v1"
	"bronivik/internal/config"
	"bronivik/internal/database"
	"bronivik/internal/models"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"
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

func TestItems(t *testing.T) {
	db := newTestDB(t)
	createTestItem(t, db, "Item A", 5)
	createTestItem(t, db, "Item B", 3)

	server := newTestHTTPServer(db)
	ts := httptest.NewServer(server.server.Handler)
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/api/v1/items")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}

	var body struct {
		Items []models.Item `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if len(body.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(body.Items))
	}
}

func TestHealthz(t *testing.T) {
	db := newTestDB(t)
	server := newTestHTTPServer(db)
	ts := httptest.NewServer(server.server.Handler)
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
}

func TestReadyz(t *testing.T) {
	db := newTestDB(t)
	server := newTestHTTPServer(db)
	ts := httptest.NewServer(server.server.Handler)
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/readyz")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
}

func TestAuth(t *testing.T) {
	db := newTestDB(t)
	cfg := config.APIConfig{
		Enabled: true,
		HTTP:    config.APIHTTPConfig{Enabled: true, Port: 0},
		Auth: config.APIAuthConfig{
			Enabled:      true,
			HeaderAPIKey: "x-api-key",
			HeaderExtra:  "x-api-extra",
			APIKeys: []config.APIClientKey{
				{Key: "valid-key", Extra: "valid-extra", Permissions: []string{"read:items"}},
			},
		},
	}
	logger := zerolog.New(io.Discard)
	server := NewHTTPServer(cfg, db, nil, nil, &logger)
	ts := httptest.NewServer(server.server.Handler)
	t.Cleanup(ts.Close)

	t.Run("MissingHeaders", func(t *testing.T) {
		resp, _ := http.Get(ts.URL + "/api/v1/items")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("InvalidKey", func(t *testing.T) {
		req, _ := http.NewRequest("GET", ts.URL+"/api/v1/items", nil)
		req.Header.Set("x-api-key", "wrong")
		req.Header.Set("x-api-extra", "valid-extra")
		resp, _ := http.DefaultClient.Do(req)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("ValidKey", func(t *testing.T) {
		req, _ := http.NewRequest("GET", ts.URL+"/api/v1/items", nil)
		req.Header.Set("x-api-key", "valid-key")
		req.Header.Set("x-api-extra", "valid-extra")
		resp, _ := http.DefaultClient.Do(req)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})
}

func newTestHTTPServer(db *database.DB) *HTTPServer {
	cfg := config.APIConfig{
		Enabled: true,
		HTTP:    config.APIHTTPConfig{Enabled: true, Port: 0},
		Auth:    config.APIAuthConfig{Enabled: false},
	}
	logger := zerolog.New(io.Discard)
	return NewHTTPServer(cfg, db, nil, nil, &logger)
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

func TestAvailabilityService_GetAvailability(t *testing.T) {
	db := newTestDB(t)
	item := createTestItem(t, db, "camera", 2)
	bookingDate := time.Date(2025, 12, 1, 10, 0, 0, 0, time.UTC)
	insertTestBooking(t, db, item, bookingDate, "confirmed")

	svc := NewAvailabilityService(db)

	t.Run("Success", func(t *testing.T) {
		req := &availabilityv1.GetAvailabilityRequest{
			ItemName: item.Name,
			Date:     bookingDate.Format("2006-01-02"),
		}
		resp, err := svc.GetAvailability(context.Background(), req)
		if err != nil {
			t.Fatalf("GetAvailability: %v", err)
		}
		if !resp.Available {
			t.Fatalf("expected available=true, got false")
		}
		if resp.BookedCount != 1 {
			t.Fatalf("expected booked_count=1, got %d", resp.BookedCount)
		}
	})

	t.Run("ItemNotFound", func(t *testing.T) {
		req := &availabilityv1.GetAvailabilityRequest{
			ItemName: "unknown",
			Date:     bookingDate.Format("2006-01-02"),
		}
		_, err := svc.GetAvailability(context.Background(), req)
		if err == nil {
			t.Fatalf("expected error for unknown item")
		}
	})

	t.Run("InvalidDate", func(t *testing.T) {
		req := &availabilityv1.GetAvailabilityRequest{
			ItemName: item.Name,
			Date:     "invalid-date",
		}
		_, err := svc.GetAvailability(context.Background(), req)
		if err == nil {
			t.Fatalf("expected error for invalid date")
		}
	})
}

func TestAvailabilityService_GetAvailabilityBulk(t *testing.T) {
	db := newTestDB(t)
	item1 := createTestItem(t, db, "camera", 2)
	item2 := createTestItem(t, db, "lens", 1)
	bookingDate := time.Date(2025, 12, 1, 10, 0, 0, 0, time.UTC)
	insertTestBooking(t, db, item1, bookingDate, "confirmed")

	svc := NewAvailabilityService(db)

	req := &availabilityv1.GetAvailabilityBulkRequest{
		Items: []string{item1.Name, item2.Name, "unknown"},
		Dates: []string{bookingDate.Format("2006-01-02")},
	}
	resp, err := svc.GetAvailabilityBulk(context.Background(), req)
	if err != nil {
		t.Fatalf("GetAvailabilityBulk: %v", err)
	}

	if len(resp.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(resp.Results))
	}
}

func TestAvailabilityService_ListItems(t *testing.T) {
	db := newTestDB(t)
	createTestItem(t, db, "camera", 2)
	createTestItem(t, db, "lens", 1)

	svc := NewAvailabilityService(db)

	resp, err := svc.ListItems(context.Background(), &availabilityv1.ListItemsRequest{})
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}

	if len(resp.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(resp.Items))
	}
}

func TestChainUnaryInterceptors(t *testing.T) {
	callCount := 0
	var calls []string

	interceptor1 := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		calls = append(calls, "interceptor1")
		return handler(ctx, req)
	}

	interceptor2 := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		calls = append(calls, "interceptor2")
		return handler(ctx, req)
	}

	handler := func(ctx context.Context, req any) (any, error) {
		callCount++
		calls = append(calls, "handler")
		return "result", nil
	}

	chained := ChainUnaryInterceptors(interceptor1, interceptor2)
	info := &grpc.UnaryServerInfo{FullMethod: "test"}

	result, err := chained(context.Background(), "request", info, handler)
	if err != nil {
		t.Fatalf("chained interceptor: %v", err)
	}

	if result != "result" {
		t.Fatalf("expected 'result', got %v", result)
	}

	if callCount != 1 {
		t.Fatalf("expected handler called once, got %d", callCount)
	}

	expected := []string{"interceptor1", "interceptor2", "handler"}
	if !reflect.DeepEqual(calls, expected) {
		t.Fatalf("expected calls %v, got %v", expected, calls)
	}
}
