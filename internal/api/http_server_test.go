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
	"strings"
	"testing"
	"time"

	availabilityv1 "bronivik/internal/api/gen/availability/v1"
	"bronivik/internal/config"
	"bronivik/internal/database"
	"bronivik/internal/models"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
)

func TestAvailabilitySuccess(t *testing.T) {
	db := newTestDB(t)
	item := createTestItem(t, db, "camera", 2)
	bookingDate := time.Date(2025, 12, 1, 10, 0, 0, 0, time.UTC)
	insertTestBooking(t, db, &item, bookingDate, "pending")

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

func TestAvailabilityBulk_Success(t *testing.T) {
	db := newTestDB(t)
	item1 := createTestItem(t, db, "camera", 2)
	createTestItem(t, db, "lens", 1)
	bookingDate := time.Date(2025, 12, 1, 10, 0, 0, 0, time.UTC)
	insertTestBooking(t, db, &item1, bookingDate, "confirmed")

	server := newTestHTTPServer(db)
	ts := httptest.NewServer(server.server.Handler)
	t.Cleanup(ts.Close)

	reqBody := `{"items":["camera","lens","unknown"],"dates":["2025-12-01"]}`
	resp, err := http.Post(ts.URL+"/api/v1/availability/bulk", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}

	var body struct {
		Results []struct {
			ItemName    string `json:"item_name"`
			Date        string `json:"date"`
			Available   bool   `json:"available"`
			BookedCount int64  `json:"booked_count"`
			Total       int64  `json:"total"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if len(body.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(body.Results))
	}

	// Check camera result
	var cameraResult *struct {
		ItemName    string `json:"item_name"`
		Date        string `json:"date"`
		Available   bool   `json:"available"`
		BookedCount int64  `json:"booked_count"`
		Total       int64  `json:"total"`
	}
	for i := range body.Results {
		if body.Results[i].ItemName == "camera" {
			cameraResult = &body.Results[i]
			break
		}
	}
	if cameraResult == nil {
		t.Fatalf("camera result not found")
	}
	if !cameraResult.Available {
		t.Fatalf("expected camera available")
	}
	if cameraResult.BookedCount != 1 {
		t.Fatalf("expected booked_count=1 for camera, got %d", cameraResult.BookedCount)
	}
}

func TestAvailabilityBulk_InvalidJSON(t *testing.T) {
	db := newTestDB(t)
	server := newTestHTTPServer(db)
	ts := httptest.NewServer(server.server.Handler)
	t.Cleanup(ts.Close)

	resp, err := http.Post(ts.URL+"/api/v1/availability/bulk", "application/json", strings.NewReader("invalid json"))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAvailabilityBulk_EmptyItems(t *testing.T) {
	db := newTestDB(t)
	server := newTestHTTPServer(db)
	ts := httptest.NewServer(server.server.Handler)
	t.Cleanup(ts.Close)

	reqBody := `{"items":[],"dates":["2025-12-01"]}`
	resp, err := http.Post(ts.URL+"/api/v1/availability/bulk", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAvailabilityBulk_EmptyDates(t *testing.T) {
	db := newTestDB(t)
	server := newTestHTTPServer(db)
	ts := httptest.NewServer(server.server.Handler)
	t.Cleanup(ts.Close)

	reqBody := `{"items":["camera"],"dates":[]}`
	resp, err := http.Post(ts.URL+"/api/v1/availability/bulk", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAvailabilityBulk_MethodNotAllowed(t *testing.T) {
	db := newTestDB(t)
	server := newTestHTTPServer(db)
	ts := httptest.NewServer(server.server.Handler)
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/availability/bulk", http.NoBody)
	resp, err := http.DefaultClient.Do(req)
	assert.NoError(t, err)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", resp.StatusCode)
	}
}

func TestAuth(t *testing.T) {
	db := newTestDB(t)
	createTestItem(t, db, "camera", 2)

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
	server := NewHTTPServer(&cfg, db, nil, nil, &logger)
	ts := httptest.NewServer(server.server.Handler)
	t.Cleanup(ts.Close)

	t.Run("MissingHeaders", func(t *testing.T) {
		req, _ := http.NewRequest("GET", ts.URL+"/api/v1/items", http.NoBody)
		resp, err := http.DefaultClient.Do(req)
		assert.NoError(t, err)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("InvalidKey", func(t *testing.T) {
		req, _ := http.NewRequest("GET", ts.URL+"/api/v1/items", http.NoBody)
		req.Header.Set("x-api-key", "wrong")
		req.Header.Set("x-api-extra", "valid-extra")
		resp, err := http.DefaultClient.Do(req)
		assert.NoError(t, err)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("ValidKey", func(t *testing.T) {
		req, _ := http.NewRequest("GET", ts.URL+"/api/v1/items", http.NoBody)
		req.Header.Set("x-api-key", "valid-key")
		req.Header.Set("x-api-extra", "valid-extra")
		resp, err := http.DefaultClient.Do(req)
		assert.NoError(t, err)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("WrongPermission", func(t *testing.T) {
		req, _ := http.NewRequest("GET", ts.URL+"/api/v1/availability/camera?date=2025-01-01", http.NoBody)
		req.Header.Set("x-api-key", "valid-key")
		req.Header.Set("x-api-extra", "valid-extra")
		resp, err := http.DefaultClient.Do(req)
		assert.NoError(t, err)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403, got %d", resp.StatusCode)
		}
	})
}

func TestAvailabilityErrors(t *testing.T) {
	db := newTestDB(t)
	server := newTestHTTPServer(db)
	ts := httptest.NewServer(server.server.Handler)
	t.Cleanup(ts.Close)

	t.Run("MethodNotAllowed", func(t *testing.T) {
		resp, err := http.Post(ts.URL+"/api/v1/availability/camera", "application/json", http.NoBody)
		assert.NoError(t, err)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("expected 405, got %d", resp.StatusCode)
		}
	})

	t.Run("MissingDate", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/v1/availability/camera")
		assert.NoError(t, err)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("InvalidDate", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/v1/availability/camera?date=invalid")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("EmptyItemName", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/v1/availability/??date=2025-01-01")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		// itemName map logic will trim ?? to empty
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})
}

func TestReadyz_DBFail(t *testing.T) {
	db := newTestDB(t)
	db.Close() // Make it fail
	server := newTestHTTPServer(db)
	ts := httptest.NewServer(server.server.Handler)
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/readyz")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", resp.StatusCode)
	}
}

func TestRateLimit(t *testing.T) {
	db := newTestDB(t)
	cfg := config.APIConfig{
		Enabled: true,
		HTTP:    config.APIHTTPConfig{Enabled: true, Port: 0},
		RateLimit: config.APIRateLimitConfig{
			RPS:   1,
			Burst: 1,
		},
	}
	logger := zerolog.New(io.Discard)
	server := NewHTTPServer(&cfg, db, nil, nil, &logger)
	ts := httptest.NewServer(server.server.Handler)
	t.Cleanup(ts.Close)

	// First request - ok
	resp1, err1 := http.Get(ts.URL + "/api/v1/items")
	if err1 != nil {
		t.Fatalf("request 1 failed: %v", err1)
	}
	defer resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp1.StatusCode)
	}

	// Second request immediately - should fail
	resp2, err2 := http.Get(ts.URL + "/api/v1/items")
	if err2 != nil {
		t.Fatalf("request 2 failed: %v", err2)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", resp2.StatusCode)
	}
}

func TestCORS(t *testing.T) {
	db := newTestDB(t)
	server := newTestHTTPServer(db)
	ts := httptest.NewServer(server.server.Handler)
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest("OPTIONS", ts.URL+"/api/v1/items", http.NoBody)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("expected CORS header")
	}
}

func TestHTTPServer_StartStop(t *testing.T) {
	db := newTestDB(t)
	cfg := config.APIConfig{
		HTTP: config.APIHTTPConfig{Enabled: true, Port: 0},
	}
	logger := zerolog.New(io.Discard)
	server := NewHTTPServer(&cfg, db, nil, nil, &logger)

	// Port 0 will bind to random port, but we need to know it to stop it if we use Start in background.
	// Actually, Start() blocks. So let's test Shutdown on unstarted server or just mock it.

	err := server.Shutdown(context.Background())
	if err != nil {
		t.Errorf("shutdown unstarted server: %v", err)
	}
}

func (s *HTTPServer) GetHandler() http.Handler {
	return s.server.Handler
}

func newTestHTTPServer(db *database.DB) *HTTPServer {
	cfg := config.APIConfig{
		Enabled: true,
		HTTP:    config.APIHTTPConfig{Enabled: true, Port: 0},
		Auth:    config.APIAuthConfig{Enabled: false},
	}
	logger := zerolog.New(io.Discard)
	return NewHTTPServer(&cfg, db, nil, nil, &logger)
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

func insertTestBooking(t *testing.T, db *database.DB, item *models.Item, date time.Time, status string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO bookings (
			user_id, user_name, user_nickname, phone, 
			item_id, item_name, date, status, comment, 
			created_at, updated_at, version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 1)
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
	insertTestBooking(t, db, &item, bookingDate, "confirmed")

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
	insertTestBooking(t, db, &item1, bookingDate, "confirmed")

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
