package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bronivik/internal/api"
	"bronivik/internal/config"
	"bronivik/internal/database"
	"bronivik/internal/logging"
	"bronivik/internal/metrics"
	"bronivik/internal/models"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"gopkg.in/yaml.v2"
)

func main() {
	// Загрузка конфигурации
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "configs/config.yaml"
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	baseLogger, closer, err := logging.New(cfg.Logging, cfg.App)
	if err != nil {
		fmt.Fprintf(os.Stderr, "init logger: %v\n", err)
		os.Exit(1)
	}
	if closer != nil {
		defer closer.Close()
	}
	logger := baseLogger.With().Str("component", "api-main").Logger()

	// Загрузка позиций из отдельного файла
	itemsPath := os.Getenv("ITEMS_PATH")
	if itemsPath == "" {
		itemsPath = "configs/items.yaml"
	}
	itemsData, err := os.ReadFile(itemsPath)
	if err != nil {
		logger.Fatal().Err(err).Str("items_path", itemsPath).Msg("read items")
	}

	var itemsConfig struct {
		Items []models.Item `yaml:"items"`
	}
	if err := yaml.Unmarshal(itemsData, &itemsConfig); err != nil {
		logger.Fatal().Err(err).Str("items_path", itemsPath).Msg("parse items")
	}

	// Инициализация базы данных
	db, err := database.NewDB(cfg.Database.Path, &logger)
	if err != nil {
		logger.Fatal().Err(err).Str("db_path", cfg.Database.Path).Msg("init database")
	}
	defer db.Close()

	// Устанавливаем items в базу данных
	db.SetItems(itemsConfig.Items)

	if !cfg.API.Enabled {
		logger.Warn().Msg("API is disabled in config, but starting API application. Check your config.")
	}

	grpcServer, err := api.NewGRPCServer(cfg.API, db, &logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("create grpc server")
	}

	httpServer := api.NewHTTPServer(cfg.API, db, &logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg.Monitoring.PrometheusEnabled {
		metrics.Register()
		if cfg.Monitoring.PrometheusPort == 0 {
			cfg.Monitoring.PrometheusPort = 9090
		}
		go startMetricsServer(ctx, cfg.Monitoring.PrometheusPort, &logger)
	}

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
