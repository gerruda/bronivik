package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"bronivik/internal/config"
	"bronivik/internal/database"
	"bronivik/internal/google"
	"bronivik/internal/metrics"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// HTTPServer exposes a lightweight HTTP API alongside the gRPC service.
type HTTPServer struct {
	cfg           *config.APIConfig
	db            *database.DB
	redisClient   *redis.Client
	sheetsService *google.SheetsService
	server        *http.Server
	auth          *HTTPAuth
	log           zerolog.Logger
}

func NewHTTPServer(
	cfg *config.APIConfig,
	db *database.DB,
	redisClient *redis.Client,
	sheetsService *google.SheetsService,
	logger *zerolog.Logger,
) *HTTPServer {
	apiMux := http.NewServeMux()
	srv := &HTTPServer{
		cfg:           cfg,
		db:            db,
		redisClient:   redisClient,
		sheetsService: sheetsService,
	}
	if logger != nil {
		srv.log = logger.With().Str("component", "http").Logger()
	}
	srv.auth = NewHTTPAuth(cfg)

	apiMux.HandleFunc("/api/v1/availability/bulk", srv.handleAvailabilityBulk)
	apiMux.HandleFunc("/api/v1/availability/", srv.handleAvailability)
	apiMux.HandleFunc("/api/v1/items", srv.handleItems)
	apiMux.HandleFunc("/healthz", srv.handleHealthz)
	apiMux.HandleFunc("/readyz", srv.handleReadyz)

	handler := srv.loggingMiddleware(corsMiddleware(srv.auth.Wrap(apiMux)))

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
	s.log.Info().Str("addr", s.server.Addr).Msg("HTTP API listening")
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
	metrics.IncHTTP("availability")
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	const prefix = "/api/v1/availability/"
	itemName := s.parseItemName(r.URL.Path, prefix)
	if itemName == "" {
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

func (s *HTTPServer) parseItemName(path, prefix string) string {
	if !strings.HasPrefix(path, prefix) {
		return ""
	}

	itemName := strings.TrimPrefix(path, prefix)
	itemName = strings.TrimSpace(itemName)

	return s.sanitizeItemName(itemName)
}

func (s *HTTPServer) sanitizeItemName(itemName string) string {
	// Простейшая очистка от потенциально опасных символов
	itemName = strings.Map(func(r rune) rune {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		isExtra := r == '-' || r == '_' || r == ' '
		isCyrillic := (r >= 'а' && r <= 'я') || (r >= 'А' && r <= 'Я')
		if isAlphaNum || isExtra || isCyrillic {
			return r
		}
		return -1
	}, itemName)

	if strings.Contains(itemName, "/") {
		return ""
	}
	return itemName
}

func (s *HTTPServer) handleAvailabilityBulk(w http.ResponseWriter, r *http.Request) {
	metrics.IncHTTP("availability_bulk")
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

	if len(body.Items) == 0 || len(body.Dates) == 0 {
		writeError(w, http.StatusBadRequest, "items and dates are required")
		return
	}

	results, err := s.processBulkAvailability(r.Context(), body.Items, body.Dates)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (s *HTTPServer) processBulkAvailability(ctx context.Context, items, dates []string) ([]map[string]any, error) {
	results := make([]map[string]any, 0, len(items)*len(dates))
	for _, rawItem := range items {
		itemName := strings.TrimSpace(rawItem)
		if itemName == "" {
			continue
		}
		for _, rawDate := range dates {
			dateStr := strings.TrimSpace(rawDate)
			if dateStr == "" {
				continue
			}
			date, err := time.Parse("2006-01-02", dateStr)
			if err != nil {
				return nil, fmt.Errorf("invalid date format: %s", dateStr)
			}

			info, err := s.db.GetItemAvailabilityByName(ctx, itemName, date)
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
	return results, nil
}

func (s *HTTPServer) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *HTTPServer) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Check Database
	if err := s.db.PingContext(ctx); err != nil {
		s.log.Error().Err(err).Msg("readyz: database ping failed")
		writeError(w, http.StatusServiceUnavailable, "database not ready")
		return
	}

	// Check Redis (if enabled)
	if s.redisClient != nil {
		if _, err := s.redisClient.Ping(ctx).Result(); err != nil {
			s.log.Error().Err(err).Msg("readyz: redis ping failed")
			writeError(w, http.StatusServiceUnavailable, "redis not ready")
			return
		}
	}

	// Check Google Sheets (if enabled)
	if s.sheetsService != nil {
		if err := s.sheetsService.TestConnection(ctx); err != nil {
			s.log.Error().Err(err).Msg("readyz: google sheets connection failed")
			writeError(w, http.StatusServiceUnavailable, "google sheets not ready")
			return
		}
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready"))
}

func (s *HTTPServer) handleItems(w http.ResponseWriter, r *http.Request) {
	metrics.IncHTTP("items")
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

// corsMiddleware adds permissive CORS headers for simple API consumption.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, x-api-key, x-api-extra")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// HTTPAuth provides API-key auth and per-key rate limiting for HTTP endpoints.
type HTTPAuth struct {
	cfg     *config.APIConfig
	clients map[string]config.APIClientKey
	limiter *rateLimiter
}

func NewHTTPAuth(cfg *config.APIConfig) *HTTPAuth {
	m := make(map[string]config.APIClientKey, len(cfg.Auth.APIKeys))
	for _, k := range cfg.Auth.APIKeys {
		m[k.Key] = k
	}
	return &HTTPAuth{
		cfg:     cfg,
		clients: m,
		limiter: newRateLimiter(cfg),
	}
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
	lim := a.limiter.getLimiter(key)
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

func (s *HTTPServer) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := requestIDFromHeader(r)
		ctx := context.WithValue(r.Context(), requestIDKey{}, reqID)
		r = r.WithContext(ctx)
		w.Header().Set(requestIDHeader, reqID)

		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		dur := time.Since(start)

		s.log.Info().
			Str("request_id", reqID).
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Str("remote", clientIP(r.RemoteAddr)).
			Int("status", recorder.status).
			Dur("duration", dur).
			Str("user_agent", r.UserAgent()).
			Msg("http request")
	})
}

const requestIDHeader = "X-Request-ID"

type requestIDKey struct{}

func requestIDFromHeader(r *http.Request) string {
	if id := strings.TrimSpace(r.Header.Get(requestIDHeader)); id != "" {
		return id
	}
	return uuid.NewString()
}

func clientIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
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
