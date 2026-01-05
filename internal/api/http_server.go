package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"bronivik/internal/config"
	"bronivik/internal/database"
	"golang.org/x/time/rate"
)

// HTTPServer exposes a lightweight HTTP API alongside the gRPC service.
type HTTPServer struct {
	cfg    config.APIConfig
	db     *database.DB
	server *http.Server
	auth   *HTTPAuth
}

func NewHTTPServer(cfg config.APIConfig, db *database.DB) *HTTPServer {
	mux := http.NewServeMux()
	srv := &HTTPServer{cfg: cfg, db: db}
	srv.auth = NewHTTPAuth(cfg)

	mux.HandleFunc("/api/v1/availability/bulk", srv.handleAvailabilityBulk)
	mux.HandleFunc("/api/v1/availability/", srv.handleAvailability)
	mux.HandleFunc("/api/v1/items", srv.handleItems)

	handler := loggingMiddleware(srv.auth.Wrap(mux))

	srv.server = &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.HTTP.Port),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      15 * time.Second,
	}

	return srv
}

func (s *HTTPServer) Start() error {
	if s.server == nil {
		return fmt.Errorf("http server is not initialized")
	}
	log.Printf("HTTP API listening on %s", s.server.Addr)
	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *HTTPServer) Shutdown(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	return s.server.Shutdown(ctx)
}

func (s *HTTPServer) handleAvailability(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	const prefix = "/api/v1/availability/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	itemName := strings.TrimPrefix(r.URL.Path, prefix)
	itemName = strings.TrimSpace(itemName)
	if itemName == "" || strings.Contains(itemName, "/") {
		writeError(w, http.StatusBadRequest, "item_name is required")
		return
	}

	dateStr := strings.TrimSpace(r.URL.Query().Get("date"))
	if dateStr == "" {
		writeError(w, http.StatusBadRequest, "date is required")
		return
	}

	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid date format; expected YYYY-MM-DD")
		return
	}

	info, err := s.db.GetItemAvailabilityByName(r.Context(), itemName, date)
	if err != nil {
		writeError(w, http.StatusNotFound, "item not found")
		return
	}

	resp := map[string]any{
		"available":    info.Available,
		"booked_count": info.BookedCount,
		"total":        info.Total,
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *HTTPServer) handleAvailabilityBulk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	type request struct {
		Items []string `json:"items"`
		Dates []string `json:"dates"`
	}

	var body request
	if r.Method == http.MethodGet {
		body.Items = splitCSV(r.URL.Query().Get("items"))
		body.Dates = splitCSV(r.URL.Query().Get("dates"))
	} else {
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}

	if len(body.Items) == 0 {
		writeError(w, http.StatusBadRequest, "items is required")
		return
	}
	if len(body.Dates) == 0 {
		writeError(w, http.StatusBadRequest, "dates is required")
		return
	}

	results := make([]map[string]any, 0, len(body.Items)*len(body.Dates))
	for _, rawItem := range body.Items {
		itemName := strings.TrimSpace(rawItem)
		if itemName == "" {
			continue
		}
		for _, rawDate := range body.Dates {
			dateStr := strings.TrimSpace(rawDate)
			if dateStr == "" {
				continue
			}
			date, err := time.Parse("2006-01-02", dateStr)
			if err != nil {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid date format: %s", dateStr))
				return
			}

			info, err := s.db.GetItemAvailabilityByName(r.Context(), itemName, date)
			if err != nil {
				// Skip unknown items to align with gRPC bulk behavior.
				continue
			}

			results = append(results, map[string]any{
				"item_name":    info.ItemName,
				"date":         dateStr,
				"available":    info.Available,
				"booked_count": info.BookedCount,
				"total":        info.Total,
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (s *HTTPServer) handleItems(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	items := s.db.GetItems()
	sort.Slice(items, func(i, j int) bool {
		if items[i].SortOrder == items[j].SortOrder {
			return items[i].ID < items[j].ID
		}
		return items[i].SortOrder < items[j].SortOrder
	})

	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// HTTPAuth provides API-key auth and per-key rate limiting for HTTP endpoints.
type HTTPAuth struct {
	cfg      config.APIConfig
	clients  map[string]config.APIClientKey
	limiters sync.Map // map[string]*rate.Limiter
}

func NewHTTPAuth(cfg config.APIConfig) *HTTPAuth {
	m := make(map[string]config.APIClientKey, len(cfg.Auth.APIKeys))
	for _, k := range cfg.Auth.APIKeys {
		m[k.Key] = k
	}
	return &HTTPAuth{cfg: cfg, clients: m}
}

func (a *HTTPAuth) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.cfg.Enabled || !a.cfg.HTTP.Enabled {
			next.ServeHTTP(w, r)
			return
		}

		if a.cfg.Auth.Enabled {
			if err := a.checkAuth(r); err != nil {
				statusCode := http.StatusUnauthorized
				if err == errPermissionDenied {
					statusCode = http.StatusForbidden
				}
				writeError(w, statusCode, err.Error())
				return
			}
		}

		if err := a.checkRateLimit(r); err != nil {
			writeError(w, http.StatusTooManyRequests, err.Error())
			return
		}

		next.ServeHTTP(w, r)
	})
}

var errPermissionDenied = fmt.Errorf("permission denied")

func (a *HTTPAuth) checkAuth(r *http.Request) error {
	apiKeyHeader := strings.TrimSpace(strings.ToLower(a.cfg.Auth.HeaderAPIKey))
	if apiKeyHeader == "" {
		apiKeyHeader = "x-api-key"
	}
	extraHeader := strings.TrimSpace(strings.ToLower(a.cfg.Auth.HeaderExtra))
	if extraHeader == "" {
		extraHeader = "x-api-extra"
	}

	apiKey := strings.TrimSpace(r.Header.Get(apiKeyHeader))
	extra := strings.TrimSpace(r.Header.Get(extraHeader))
	if apiKey == "" || extra == "" {
		return fmt.Errorf("missing api key headers")
	}

	client, ok := a.clients[apiKey]
	if !ok {
		return fmt.Errorf("invalid api key")
	}
	if subtle.ConstantTimeCompare([]byte(client.Extra), []byte(extra)) != 1 {
		return fmt.Errorf("invalid extra header")
	}

	if err := a.checkPermissions(client, r); err != nil {
		return err
	}

	return nil
}

func (a *HTTPAuth) checkPermissions(client config.APIClientKey, r *http.Request) error {
	required := requiredPermissionHTTP(r)
	if required == "" {
		return nil
	}
	if len(client.Permissions) == 0 {
		return nil
	}
	for _, p := range client.Permissions {
		if strings.TrimSpace(p) == required {
			return nil
		}
	}
	return errPermissionDenied
}

func requiredPermissionHTTP(r *http.Request) string {
	path := r.URL.Path
	if strings.HasPrefix(path, "/api/v1/availability") {
		return "read:availability"
	}
	if path == "/api/v1/items" {
		return "read:items"
	}
	return ""
}

func (a *HTTPAuth) checkRateLimit(r *http.Request) error {
	if a.cfg.RateLimit.RPS <= 0 {
		return nil
	}

	key := a.clientKey(r)
	lim := a.getLimiter(key)
	if !lim.Allow() {
		return fmt.Errorf("rate limit exceeded")
	}
	return nil
}

func (a *HTTPAuth) clientKey(r *http.Request) string {
	apiKeyHeader := strings.TrimSpace(strings.ToLower(a.cfg.Auth.HeaderAPIKey))
	if apiKeyHeader == "" {
		apiKeyHeader = "x-api-key"
	}

	if apiKey := strings.TrimSpace(r.Header.Get(apiKeyHeader)); apiKey != "" {
		return apiKey
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return "unknown"
}

func (a *HTTPAuth) getLimiter(key string) *rate.Limiter {
	if v, ok := a.limiters.Load(key); ok {
		return v.(*rate.Limiter)
	}

	burst := a.cfg.RateLimit.Burst
	if burst <= 0 {
		burst = 5
	}

	lim := rate.NewLimiter(rate.Limit(a.cfg.RateLimit.RPS), burst)
	actual, loaded := a.limiters.LoadOrStore(key, lim)
	if loaded {
		return actual.(*rate.Limiter)
	}
	return lim
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		dur := time.Since(start)
		log.Printf("http method=%s path=%s status=%d dur=%s", r.Method, r.URL.Path, recorder.status, dur)
	})
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, statusCode int, message string) {
	writeJSON(w, statusCode, map[string]string{"error": message})
}

func splitCSV(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
