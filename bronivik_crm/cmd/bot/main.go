package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	crmapi "bronivik/bronivik_crm/internal/api"
	"bronivik/bronivik_crm/internal/bot"
	"bronivik/bronivik_crm/internal/database"
	"bronivik/bronivik_crm/internal/metrics"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Telegram struct {
		BotToken string `yaml:"bot_token"`
		Debug    bool   `yaml:"debug"`
	} `yaml:"telegram"`

	Database struct {
		Path string `yaml:"path"`
	} `yaml:"database"`

	Redis struct {
		Address  string `yaml:"address"`
		Password string `yaml:"password"`
		DB       int    `yaml:"db"`
	} `yaml:"redis"`

	API struct {
		BaseURL         string `yaml:"base_url"`
		APIKey          string `yaml:"api_key"`
		APIExtra        string `yaml:"api_extra"`
		CacheTTLSeconds int    `yaml:"cache_ttl_seconds"`
	} `yaml:"api"`

	Monitoring struct {
		HealthCheckPort   int  `yaml:"health_check_port"`
		PrometheusEnabled bool `yaml:"prometheus_enabled"`
		PrometheusPort    int  `yaml:"prometheus_port"`
	} `yaml:"monitoring"`

	Booking struct {
		MinAdvanceMinutes int `yaml:"min_advance_minutes"`
		MaxAdvanceDays    int `yaml:"max_advance_days"`
		MaxActivePerUser  int `yaml:"max_active_per_user"`
	} `yaml:"booking"`

	Managers []int64 `yaml:"managers"`
}

func main() {
	// Initialize logger
	output := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
	logger := zerolog.New(output).With().Timestamp().Logger()

	configPath := os.Getenv("CRM_CONFIG_PATH")
	if configPath == "" {
		configPath = "configs/config.yaml"
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		logger.Fatal().Err(err).Msg("read config error")
	}

	// Support ${ENV_VAR} placeholders in YAML config.
	data = []byte(os.ExpandEnv(string(data)))

	var cfg Config
	if err = yaml.Unmarshal(data, &cfg); err != nil {
		logger.Fatal().Err(err).Msg("parse config error")
	}
	if cfg.Telegram.BotToken == "" || cfg.Telegram.BotToken == "YOUR_BOT_TOKEN_HERE" {
		logger.Fatal().Msg("set telegram.bot_token in config")
	}
	if cfg.Database.Path == "" {
		cfg.Database.Path = "data/bronivik_crm.db"
	}
	if err = os.MkdirAll(filepath.Dir(cfg.Database.Path), 0o755); err != nil {
		logger.Fatal().Err(err).Msg("mkdir db dir error")
	}

	db, err := database.NewDB(cfg.Database.Path)
	if err != nil {
		logger.Fatal().Err(err).Msg("open db error")
	}
	defer db.Close()

	client := crmapi.NewBronivikClient(cfg.API.BaseURL, cfg.API.APIKey, cfg.API.APIExtra)
	var rdb *redis.Client
	if cfg.Redis.Address != "" && cfg.API.CacheTTLSeconds > 0 {
		rdb = redis.NewClient(&redis.Options{Addr: cfg.Redis.Address, Password: cfg.Redis.Password, DB: cfg.Redis.DB})
		client.UseRedisCache(rdb, time.Duration(cfg.API.CacheTTLSeconds)*time.Second)
	}

	rules := botRulesFromConfig(&cfg)
	b, err := bot.New(cfg.Telegram.BotToken, client, db, cfg.Managers, rules, &logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("create bot error")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg.Monitoring.HealthCheckPort == 0 {
		cfg.Monitoring.HealthCheckPort = 8090
	}
	go startHealthServer(ctx, cfg.Monitoring.HealthCheckPort, db, rdb, &logger)

	if cfg.Monitoring.PrometheusEnabled {
		if cfg.Monitoring.PrometheusPort == 0 {
			cfg.Monitoring.PrometheusPort = 9090
		}
		metrics.Register()
		go startMetricsServer(ctx, cfg.Monitoring.PrometheusPort, &logger)
	}

	logger.Info().Msg("CRM bot started")
	b.Start(ctx)
}

func botRulesFromConfig(cfg *Config) bot.BookingRules {
	minAdvance := 60 * time.Minute
	if cfg.Booking.MinAdvanceMinutes > 0 {
		minAdvance = time.Duration(cfg.Booking.MinAdvanceMinutes) * time.Minute
	}
	maxAdvance := 30 * 24 * time.Hour
	if cfg.Booking.MaxAdvanceDays > 0 {
		maxAdvance = time.Duration(cfg.Booking.MaxAdvanceDays) * 24 * time.Hour
	}
	return bot.BookingRules{MinAdvance: minAdvance, MaxAdvance: maxAdvance, MaxActivePerUser: cfg.Booking.MaxActivePerUser}
}

func startHealthServer(ctx context.Context, port int, db *database.DB, rdb *redis.Client, logger *zerolog.Logger) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		ctxPing, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		if err := db.PingContext(ctxPing); err != nil {
			http.Error(w, "db not ready", http.StatusServiceUnavailable)
			return
		}
		if rdb != nil {
			if err := rdb.Ping(ctxPing).Err(); err != nil {
				http.Error(w, "redis not ready", http.StatusServiceUnavailable)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})

	srv := &http.Server{Addr: fmt.Sprintf(":%d", port), Handler: mux}
	go func() {
		<-ctx.Done()
		ctxShutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctxShutdown)
	}()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error().Err(err).Msg("health server error")
	}
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
