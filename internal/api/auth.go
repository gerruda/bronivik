package api

import (
	"context"
	"crypto/subtle"
	"strings"
	"sync"
	"time"

	"bronivik/internal/config"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"golang.org/x/time/rate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

type AuthInterceptor struct {
	cfg *config.APIConfig

	clientsByAPIKey map[string]config.APIClientKey
	limiters        sync.Map // map[string]*rate.Limiter
}

func NewAuthInterceptor(cfg *config.APIConfig) *AuthInterceptor {
	m := make(map[string]config.APIClientKey, len(cfg.Auth.APIKeys))
	for _, k := range cfg.Auth.APIKeys {
		m[k.Key] = k
	}

	return &AuthInterceptor{cfg: cfg, clientsByAPIKey: m}
}

func (a *AuthInterceptor) Unary() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if !a.cfg.Enabled {
			return handler(ctx, req)
		}

		if a.cfg.Auth.Enabled {
			if err := a.checkAuth(ctx, info.FullMethod); err != nil {
				return nil, err
			}
		}
		if err := a.checkRateLimit(ctx); err != nil {
			return nil, err
		}

		return handler(ctx, req)
	}
}

func (a *AuthInterceptor) checkAuth(ctx context.Context, fullMethod string) error {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "missing metadata")
	}

	apiKeyHeader := strings.ToLower(strings.TrimSpace(a.cfg.Auth.HeaderAPIKey))
	if apiKeyHeader == "" {
		apiKeyHeader = "x-api-key"
	}

	extraHeader := strings.ToLower(strings.TrimSpace(a.cfg.Auth.HeaderExtra))
	if extraHeader == "" {
		extraHeader = "x-api-extra"
	}

	apiKey := first(md.Get(apiKeyHeader))
	extra := first(md.Get(extraHeader))
	if apiKey == "" || extra == "" {
		return status.Error(codes.Unauthenticated, "missing api key headers")
	}

	client, ok := a.clientsByAPIKey[apiKey]
	if !ok {
		return status.Error(codes.Unauthenticated, "invalid api key")
	}

	if subtle.ConstantTimeCompare([]byte(client.Extra), []byte(extra)) != 1 {
		return status.Error(codes.Unauthenticated, "invalid extra header")
	}

	if err := a.checkPermissions(client, fullMethod); err != nil {
		return err
	}

	return nil
}

func (a *AuthInterceptor) checkPermissions(client config.APIClientKey, fullMethod string) error {
	required := requiredPermission(fullMethod)
	if required == "" {
		return nil
	}

	// If permissions list is empty, treat as allow-all.
	if len(client.Permissions) == 0 {
		return nil
	}

	for _, p := range client.Permissions {
		if strings.TrimSpace(p) == required {
			return nil
		}
	}
	return status.Error(codes.PermissionDenied, "permission denied")
}

func requiredPermission(fullMethod string) string {
	switch fullMethod {
	case "/bronivik.availability.v1.AvailabilityService/GetAvailability":
		return "read:availability"
	case "/bronivik.availability.v1.AvailabilityService/GetAvailabilityBulk":
		return "read:availability"
	case "/bronivik.availability.v1.AvailabilityService/ListItems":
		return "read:items"
	default:
		return ""
	}
}

func (a *AuthInterceptor) checkRateLimit(ctx context.Context) error {
	if a.cfg.RateLimit.RPS <= 0 {
		return nil
	}

	key := a.clientKey(ctx)
	lim := a.getLimiter(key)
	if !lim.Allow() {
		return status.Error(codes.ResourceExhausted, "rate limit exceeded")
	}
	return nil
}

func (a *AuthInterceptor) clientKey(ctx context.Context) string {
	md, _ := metadata.FromIncomingContext(ctx)
	apiKeyHeader := strings.ToLower(strings.TrimSpace(a.cfg.Auth.HeaderAPIKey))
	if apiKeyHeader == "" {
		apiKeyHeader = "x-api-key"
	}
	apiKey := first(md.Get(apiKeyHeader))
	if apiKey != "" {
		return apiKey
	}

	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		return p.Addr.String()
	}
	return "unknown"
}

func (a *AuthInterceptor) getLimiter(key string) *rate.Limiter {
	if v, ok := a.limiters.Load(key); ok {
		if lim, ok := v.(*rate.Limiter); ok {
			return lim
		}
	}

	burst := a.cfg.RateLimit.Burst
	if burst <= 0 {
		burst = 5
	}

	lim := rate.NewLimiter(rate.Limit(a.cfg.RateLimit.RPS), burst)
	actual, loaded := a.limiters.LoadOrStore(key, lim)
	if loaded {
		if lim, ok := actual.(*rate.Limiter); ok {
			return lim
		}
	}
	return lim
}

func first(vals []string) string {
	if len(vals) == 0 {
		return ""
	}
	return strings.TrimSpace(vals[0])
}

func LoggingUnaryInterceptor(logger *zerolog.Logger) grpc.UnaryServerInterceptor {
	base := zerolog.Nop()
	if logger != nil {
		base = logger.With().Str("component", "grpc").Logger()
	}

	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		requestID := requestIDFromMetadata(ctx)
		_ = grpc.SetHeader(ctx, metadata.Pairs(requestIDMetadataKey, requestID))

		start := time.Now()
		resp, err := handler(ctx, req)
		dur := time.Since(start)

		code := codes.OK
		if err != nil {
			code = status.Code(err)
		}

		remote := "unknown"
		if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
			remote = p.Addr.String()
		}

		base.Info().
			Str("request_id", requestID).
			Str("method", info.FullMethod).
			Str("remote", remote).
			Str("code", code.String()).
			Dur("duration", dur).
			Msg("grpc request")

		return resp, err
	}
}

const requestIDMetadataKey = "x-request-id"

func requestIDFromMetadata(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if ok {
		if vals := md.Get(requestIDMetadataKey); len(vals) > 0 {
			if id := strings.TrimSpace(vals[0]); id != "" {
				return id
			}
		}
	}
	return uuid.NewString()
}
