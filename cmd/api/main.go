package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bronivik/internal/api"
	"bronivik/internal/config"
	"bronivik/internal/database"
	"bronivik/internal/models"
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
		log.Fatalf("Error loading config: %v", err)
	}

	// Загрузка позиций из отдельного файла
	itemsPath := os.Getenv("ITEMS_PATH")
	if itemsPath == "" {
		itemsPath = "configs/items.yaml"
	}
	itemsData, err := os.ReadFile(itemsPath)
	if err != nil {
		log.Fatalf("Ошибка чтения %s: %v", itemsPath, err)
	}

	var itemsConfig struct {
		Items []models.Item `yaml:"items"`
	}
	if err := yaml.Unmarshal(itemsData, &itemsConfig); err != nil {
		log.Fatalf("Ошибка парсинга %s: %v", itemsPath, err)
	}

	// Инициализация базы данных
	db, err := database.NewDB(cfg.Database.Path)
	if err != nil {
		log.Fatalf("Ошибка инициализации базы данных: %v", err)
	}
	defer db.Close()

	// Устанавливаем items в базу данных
	db.SetItems(itemsConfig.Items)

	if !cfg.API.Enabled {
		log.Println("API is disabled in config, but starting API application. Check your config.")
	}

	grpcServer, err := api.NewGRPCServer(cfg.API, db)
	if err != nil {
		log.Fatalf("Failed to create gRPC API server: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := grpcServer.Serve(); err != nil {
			log.Printf("gRPC server stopped: %v", err)
		}
	}()

	log.Printf("API server started on %s", grpcServer.Addr())

	<-ctx.Done()
	log.Println("Shutdown signal received...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	grpcServer.Shutdown(shutdownCtx)
	log.Println("API server stopped")
}
