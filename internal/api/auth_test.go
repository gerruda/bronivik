package api

import (
	"context"
	"testing"

	"bronivik/internal/config"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestAuthInterceptor(t *testing.T) {
	cfg := config.APIConfig{
		Enabled: true,
		Auth: config.APIAuthConfig{
			Enabled:      true,
			HeaderAPIKey: "x-api-key",
			HeaderExtra:  "x-api-extra",
			APIKeys: []config.APIClientKey{
				{Key: "valid-key", Extra: "valid-extra", Permissions: []string{"read:items"}},
			},
		},
		RateLimit: config.APIRateLimitConfig{
			RPS:   100,
			Burst: 200,
		},
	}

	auth := NewAuthInterceptor(&cfg)
	interceptor := auth.Unary()

	handler := func(_ context.Context, req any) (any, error) {
		return "ok", nil
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/bronivik.availability.v1.AvailabilityService/ListItems"}

	t.Run("Success", func(t *testing.T) {
		md := metadata.Pairs("x-api-key", "valid-key", "x-api-extra", "valid-extra")
		ctx := metadata.NewIncomingContext(context.Background(), md)
		resp, err := interceptor(ctx, "req", info, handler)
		assert.NoError(t, err)
		assert.Equal(t, "ok", resp)
	})

	t.Run("MissingMetadata", func(t *testing.T) {
		_, err := interceptor(context.Background(), "req", info, handler)
		assert.Error(t, err)
		assert.Equal(t, codes.Unauthenticated, status.Code(err))
	})

	t.Run("MissingHeaders", func(t *testing.T) {
		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs())
		_, err := interceptor(ctx, "req", info, handler)
		assert.Error(t, err)
		assert.Equal(t, codes.Unauthenticated, status.Code(err))
	})

	t.Run("InvalidKey", func(t *testing.T) {
		md := metadata.Pairs("x-api-key", "invalid", "x-api-extra", "valid-extra")
		ctx := metadata.NewIncomingContext(context.Background(), md)
		_, err := interceptor(ctx, "req", info, handler)
		assert.Error(t, err)
		assert.Equal(t, codes.Unauthenticated, status.Code(err))
	})

	t.Run("InvalidExtra", func(t *testing.T) {
		md := metadata.Pairs("x-api-key", "valid-key", "x-api-extra", "invalid")
		ctx := metadata.NewIncomingContext(context.Background(), md)
		_, err := interceptor(ctx, "req", info, handler)
		assert.Error(t, err)
		assert.Equal(t, codes.Unauthenticated, status.Code(err))
	})

	t.Run("PermissionDenied", func(t *testing.T) {
		md := metadata.Pairs("x-api-key", "valid-key", "x-api-extra", "valid-extra")
		ctx := metadata.NewIncomingContext(context.Background(), md)
		badInfo := &grpc.UnaryServerInfo{FullMethod: "/bronivik.availability.v1.AvailabilityService/GetAvailability"}
		_, err := interceptor(ctx, "req", badInfo, handler)
		assert.Error(t, err)
		assert.Equal(t, codes.PermissionDenied, status.Code(err))
	})
}

func TestAuthInterceptor_RateLimit(t *testing.T) {
	cfg := config.APIConfig{
		Enabled: true,
		Auth:    config.APIAuthConfig{Enabled: false},
		RateLimit: config.APIRateLimitConfig{
			RPS:   1,
			Burst: 1,
		},
	}

	auth := NewAuthInterceptor(&cfg)
	interceptor := auth.Unary()
	info := &grpc.UnaryServerInfo{FullMethod: "test"}
	handler := func(_ context.Context, req any) (any, error) { return "ok", nil }

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-api-key", "key1"))

	// First request - ok
	_, err := interceptor(ctx, "req", info, handler)
	assert.NoError(t, err)

	// Second request - blocked
	_, err = interceptor(ctx, "req", info, handler)
	assert.Error(t, err)
	assert.Equal(t, codes.ResourceExhausted, status.Code(err))
}

func TestLoggingUnaryInterceptor(t *testing.T) {
	interceptor := LoggingUnaryInterceptor(nil)
	handler := func(ctx context.Context, req any) (any, error) {
		return "ok", nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "test"}

	// Test basic execution
	resp, err := interceptor(context.Background(), "req", info, handler)
	assert.NoError(t, err)
	assert.Equal(t, "ok", resp)
}

func TestRequiredPermission(t *testing.T) {
	tests := []struct {
		method string
		want   string
	}{
		{"/bronivik.availability.v1.AvailabilityService/GetAvailability", "read:availability"},
		{"/bronivik.availability.v1.AvailabilityService/GetAvailabilityBulk", "read:availability"},
		{"/bronivik.availability.v1.AvailabilityService/ListItems", "read:items"},
		{"other", ""},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, requiredPermission(tt.method))
	}
}
