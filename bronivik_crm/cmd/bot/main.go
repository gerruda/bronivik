package main

import (
	"context"
	"log"
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
	if cfg.Redis.Address != "" && cfg.API.CacheTTLSeconds > 0 {
		rdb := redis.NewClient(&redis.Options{Addr: cfg.Redis.Address, Password: cfg.Redis.Password, DB: cfg.Redis.DB})
		client.UseRedisCache(rdb, time.Duration(cfg.API.CacheTTLSeconds)*time.Second)
	}

	b, err := bot.New(cfg.Telegram.BotToken, client, db, cfg.Managers)
	if err != nil {
		log.Fatalf("create bot: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Println("CRM bot started")
	b.Start(ctx)
}
