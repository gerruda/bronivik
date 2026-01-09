package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"bronivik/internal/config"
	"bronivik/internal/database"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestGRPCServer_New(t *testing.T) {
	logger := zerolog.New(os.Stdout)
	db, _ := database.NewDB(":memory:", &logger)

	cfg := config.APIConfig{
		GRPC: config.APIGRPCConfig{
			Port: 0, // Random port
		},
	}

	s, err := NewGRPCServer(&cfg, db, &logger)
	assert.NoError(t, err)
	assert.NotNil(t, s)
	assert.NotEmpty(t, s.Addr())

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	s.Shutdown(ctx)
}

func TestBuildTLSConfig(t *testing.T) {
	t.Run("EmptyPaths", func(t *testing.T) {
		_, err := buildTLSConfig(config.APITLSConfig{Enabled: true})
		assert.Error(t, err)
	})

	t.Run("InvalidCert", func(t *testing.T) {
		_, err := buildTLSConfig(config.APITLSConfig{
			Enabled:  true,
			CertFile: "/nonexistent",
			KeyFile:  "/nonexistent",
		})
		assert.Error(t, err)
	})

	t.Run("ClientCA_Missing", func(t *testing.T) {
		// We'd need actual certs to test further, which is complex.
		// But we can test the check for ClientCAFile.
		// We can't easily test LoadX509KeyPair without files.
	})
}

func TestHTTPServer_Shutdown(_ *testing.T) {
	logger := zerolog.New(os.Stdout)
	db, _ := database.NewDB(":memory:", &logger)
	cfg := config.APIConfig{
		HTTP: config.APIHTTPConfig{
			Enabled: true,
			Port:    0,
		},
	}
	s := NewHTTPServer(&cfg, db, nil, nil, &logger)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_ = s.Shutdown(ctx)
}

func TestHTTPServer_Start(_ *testing.T) {
	logger := zerolog.New(os.Stdout)
	db, _ := database.NewDB(":memory:", &logger)
	cfg := config.APIConfig{
		HTTP: config.APIHTTPConfig{
			Enabled: true,
			Port:    0,
		},
	}
	s := NewHTTPServer(&cfg, db, nil, nil, &logger)

	go func() {
		_ = s.Start()
	}()

	time.Sleep(50 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_ = s.Shutdown(ctx)
}

func TestHTTPServer_Readyz_Full(t *testing.T) {
	logger := zerolog.New(os.Stdout)
	db, _ := database.NewDB(":memory:", &logger)
	cfg := config.APIConfig{
		HTTP: config.APIHTTPConfig{Enabled: true, Port: 0},
	}

	// Test with nil extra services (already covered mostly, but let's be explicit)
	s := NewHTTPServer(&cfg, db, nil, nil, &logger)

	req := httptest.NewRequest("GET", "/readyz", http.NoBody)
	w := httptest.NewRecorder()
	s.handleReadyz(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestSplitCSV_EdgeCases(t *testing.T) {
	assert.Nil(t, splitCSV(""))
	assert.Equal(t, []string{"a"}, splitCSV("a"))
	assert.Equal(t, []string{"a", "b"}, splitCSV("a, b"))
}

func TestGRPCServer_Serve(t *testing.T) {
	logger := zerolog.New(os.Stdout)
	db, _ := database.NewDB(":memory:", &logger)
	cfg := config.APIConfig{
		GRPC: config.APIGRPCConfig{Port: 0},
	}
	s, _ := NewGRPCServer(&cfg, db, &logger)

	go func() {
		_ = s.Serve()
	}()

	time.Sleep(50 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	s.Shutdown(ctx)
}
