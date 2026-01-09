package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bronivik/internal/api"
	"bronivik/internal/config"
	"bronivik/internal/database"
	"bronivik/internal/google"
	"bronivik/internal/logging"
	"bronivik/internal/metrics"
	"bronivik/internal/models"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"gopkg.in/yaml.v2"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("Fatal error: %v", err)
	}
}

func run() error {
	cfg, logger, closer, err := loadConfigAndLogger()
	if err != nil {
		return err
	}
	if closer != nil {
		defer (func() { _ = closer.Close() })()
	}

	items, err := loadItems(&logger)
	if err != nil {
		return err
	}

	db, err := initDatabase(cfg, items, &logger)
	if err != nil {
		return err
	}
	defer db.Close()

	if !cfg.API.Enabled {
		logger.Warn().Msg("API is disabled in config, but starting API application. Check your config.")
	}

	redisClient := initRedis(cfg, &logger)
	if redisClient != nil {
		defer redisClient.Close()
	}

	sheetsService := initGoogleSheets(cfg, &logger)

	grpcServer, err := api.NewGRPCServer(&cfg.API, db, &logger)
	if err != nil {
		logger.Error().Err(err).Msg("create grpc server")
		return err
	}

	httpServer := api.NewHTTPServer(&cfg.API, db, redisClient, sheetsService, &logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	startMetrics(ctx, cfg, &logger)

	return startServers(ctx, grpcServer, httpServer, cfg, &logger)
}

func loadConfigAndLogger() (*config.Config, zerolog.Logger, io.Closer, error) {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "configs/config.yaml"
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, zerolog.Logger{}, nil, fmt.Errorf("load config: %w", err)
	}

	baseLogger, closer, err := logging.New(cfg.Logging, cfg.App)
	if err != nil {
		return nil, zerolog.Logger{}, nil, fmt.Errorf("init logger: %w", err)
	}
	logger := baseLogger.With().Str("component", "api-main").Logger()

	return cfg, logger, closer, nil
}

func loadItems(logger *zerolog.Logger) ([]models.Item, error) {
	itemsPath := os.Getenv("ITEMS_PATH")
	if itemsPath == "" {
		itemsPath = "configs/items.yaml"
	}
	itemsData, err := os.ReadFile(itemsPath)
	if err != nil {
		logger.Error().Err(err).Str("items_path", itemsPath).Msg("read items")
		return nil, err
	}

	var itemsConfig struct {
		Items []models.Item `yaml:"items"`
	}
	if err := yaml.Unmarshal(itemsData, &itemsConfig); err != nil {
		logger.Error().Err(err).Str("items_path", itemsPath).Msg("parse items")
		return nil, err
	}

	return itemsConfig.Items, nil
}

func initDatabase(cfg *config.Config, items []models.Item, logger *zerolog.Logger) (*database.DB, error) {
	db, err := database.NewDB(cfg.Database.Path, logger)
	if err != nil {
		logger.Error().Err(err).Str("db_path", cfg.Database.Path).Msg("init database")
		return nil, err
	}

	itemPointers := make([]*models.Item, len(items))
	for i := range items {
		itemPointers[i] = &items[i]
	}
	db.SetItems(itemPointers)
	return db, nil
}

func initRedis(cfg *config.Config, logger *zerolog.Logger) *redis.Client {
	if cfg.Redis.Address == "" {
		return nil
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Address,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
		PoolSize: cfg.Redis.PoolSize,
	})

	if _, err := redisClient.Ping(context.Background()).Result(); err != nil {
		logger.Warn().Err(err).Msg("redis connection failed, continuing without redis")
		return nil
	}

	logger.Info().Str("addr", cfg.Redis.Address).Msg("redis connected")
	return redisClient
}

func initGoogleSheets(cfg *config.Config, logger *zerolog.Logger) *google.SheetsService {
	if cfg.Google.GoogleCredentialsFile == "" || cfg.Google.BookingSpreadSheetID == "" {
		return nil
	}

	sheetsService, err := google.NewSimpleSheetsService(
		cfg.Google.GoogleCredentialsFile,
		cfg.Google.UsersSpreadSheetID,
		cfg.Google.BookingSpreadSheetID,
	)
	if err != nil {
		logger.Warn().Err(err).Msg("google sheets init failed, continuing without sheets")
		return nil
	}

	logger.Info().Msg("google sheets connected")
	return sheetsService
}

func startMetrics(ctx context.Context, cfg *config.Config, logger *zerolog.Logger) {
	if !cfg.Monitoring.PrometheusEnabled {
		return
	}

	metrics.Register()
	port := cfg.Monitoring.PrometheusPort
	if port == 0 {
		port = 9090
	}
	go startMetricsServer(ctx, port, logger)
}

func startServers(
	ctx context.Context,
	grpcServer *api.GRPCServer,
	httpServer *api.HTTPServer,
	cfg *config.Config,
	logger *zerolog.Logger,
) error {
	go func() {
		if err := grpcServer.Serve(); err != nil {
			logger.Error().Err(err).Msg("grpc server stopped")
		}
	}()

	go func() {
		if !cfg.API.HTTP.Enabled {
			return
		}
		if err := httpServer.Start(); err != nil {
			logger.Error().Err(err).Msg("http server stopped")
		}
	}()

	logger.Info().Str("grpc_addr", grpcServer.Addr()).Int("http_port", cfg.API.HTTP.Port).Msg("API server started")

	<-ctx.Done()
	logger.Info().Msg("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	grpcServer.Shutdown(shutdownCtx)
	_ = httpServer.Shutdown(shutdownCtx)

	logger.Info().Msg("API server stopped")
	return nil
}

func startMetricsServer(ctx context.Context, port int, logger *zerolog.Logger) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	srv := &http.Server{Addr: fmt.Sprintf(":%d", port), Handler: mux}
	go func() {
		<-ctx.Done()
		ctxShutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctxShutdown)
	}()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error().Err(err).Msg("metrics server error")
	}
}
