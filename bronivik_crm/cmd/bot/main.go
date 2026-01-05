package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	crmapi "bronivik/bronivik_crm/internal/api"
	"bronivik/bronivik_crm/internal/bot"
	"bronivik/bronivik_crm/internal/database"
	"github.com/redis/go-redis/v9"
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
		CacheTTLSeconds int    `yaml:"cache_ttl_seconds"`
	} `yaml:"api"`

	Monitoring struct {
		HealthCheckPort int `yaml:"health_check_port"`
	} `yaml:"monitoring"`

	Booking struct {
		MinAdvanceMinutes int `yaml:"min_advance_minutes"`
		MaxAdvanceDays    int `yaml:"max_advance_days"`
		MaxActivePerUser  int `yaml:"max_active_per_user"`
	} `yaml:"booking"`

	Managers []int64 `yaml:"managers"`
}

func main() {
	configPath := os.Getenv("CRM_CONFIG_PATH")
	if configPath == "" {
		configPath = "configs/config.yaml"
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		log.Fatalf("read config: %v", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		log.Fatalf("parse config: %v", err)
	}
	if cfg.Telegram.BotToken == "" || cfg.Telegram.BotToken == "YOUR_BOT_TOKEN_HERE" {
		log.Fatal("set telegram.bot_token in config")
	}
	if cfg.Database.Path == "" {
		cfg.Database.Path = "data/bronivik_crm.db"
	}
	if err := os.MkdirAll(filepath.Dir(cfg.Database.Path), 0o755); err != nil {
		log.Fatalf("mkdir db dir: %v", err)
	}

	db, err := database.NewDB(cfg.Database.Path)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	client := crmapi.NewBronivikClient(cfg.API.BaseURL, cfg.API.APIKey)
	var rdb *redis.Client
	if cfg.Redis.Address != "" && cfg.API.CacheTTLSeconds > 0 {
		rdb = redis.NewClient(&redis.Options{Addr: cfg.Redis.Address, Password: cfg.Redis.Password, DB: cfg.Redis.DB})
		client.UseRedisCache(rdb, time.Duration(cfg.API.CacheTTLSeconds)*time.Second)
	}

	rules := botRulesFromConfig(cfg)
	b, err := bot.New(cfg.Telegram.BotToken, client, db, cfg.Managers, rules)
	if err != nil {
		log.Fatalf("create bot: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg.Monitoring.HealthCheckPort == 0 {
		cfg.Monitoring.HealthCheckPort = 8090
	}
	go startHealthServer(ctx, cfg.Monitoring.HealthCheckPort, db, rdb)

	log.Println("CRM bot started")
	b.Start(ctx)
}

func botRulesFromConfig(cfg Config) bot.BookingRules {
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

func startHealthServer(ctx context.Context, port int, db *database.DB, rdb *redis.Client) {
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
		log.Printf("health server error: %v", err)
	}
}
