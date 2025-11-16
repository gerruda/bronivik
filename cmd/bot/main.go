package main

import (
	"log"
	"os"
	"path/filepath"

	"bronivik/internal/bot"
	"bronivik/internal/config"
	"bronivik/internal/database"
	"bronivik/internal/google"
	"bronivik/internal/models"
	"gopkg.in/yaml.v2"
)

func main() {
	// Загрузка конфигурации
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "configs/config.yaml"
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		log.Fatalf("Config file does not exist: %s", configPath)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	if _, err := os.Stat("configs/items.yaml"); os.IsNotExist(err) {
		log.Fatalf("Config file does not exist: %s", "configs/items.yaml")
	}

	// Загрузка позиций из отдельного файла
	itemsData, err := os.ReadFile("configs/items.yaml")
	if err != nil {
		log.Fatal("Ошибка чтения items.yaml:", err)
	}

	var itemsConfig struct {
		Items []models.Item `yaml:"items"`
	}
	if err := yaml.Unmarshal(itemsData, &itemsConfig); err != nil {
		log.Fatal("Ошибка парсинга items.yaml:", err)
	}

	// Создаем необходимые директории
	if cfg == nil {
		log.Fatal("Cfg configuration is missing in config")
	}
	if err := os.MkdirAll(filepath.Dir(cfg.Database.Path), 0755); err != nil {
		log.Fatal("Ошибка создания директории для базы данных:", err)
	}

	if err := os.MkdirAll(cfg.Exports.Path, 0755); err != nil {
		log.Fatal("Ошибка создания директории для экспорта:", err)
	}

	// Инициализация базы данных
	db, err := database.NewDB(cfg.Database.Path)
	if err != nil {
		log.Fatal("Ошибка инициализации базы данных:", err)
	}
	defer db.Close()

	// Устанавливаем items в базу данных
	db.SetItems(itemsConfig.Items)

	if cfg.Telegram.BotToken == "YOUR_BOT_TOKEN_HERE" {
		log.Fatal("Задайте токен бота в config.yaml")
	}

	// Инициализация Google Sheets через API Key
	var sheetsService *google.SheetsService
	if cfg.Google.GoogleCredentialsFile == "" || cfg.Google.UsersSpreadSheetId == "" || cfg.Google.BookingSpreadSheetId == "" {
		log.Fatal("Нехватает переменных для подключения к Гуглу", err)
	}

	service, err := google.NewSimpleSheetsService(
		cfg.Google.GoogleCredentialsFile,
		cfg.Google.UsersSpreadSheetId,
		cfg.Google.BookingSpreadSheetId,
	)
	if err != nil {
		log.Printf("Warning: Failed to initialize Google Sheets service: %v", err)
	}

	// Тестируем подключение
	if err := service.TestConnection(); err != nil {
		log.Fatalf("Warning: Google Sheets connection test failed: %v", err)
	} else {
		sheetsService = service
		log.Println("Google Sheets service initialized successfully")
	}

	// Создание и запуск бота
	telegramBot, err := bot.NewBot(cfg.Telegram.BotToken, cfg, itemsConfig.Items, db, sheetsService)
	if err != nil {
		log.Fatal("Ошибка создания бота:", err)
	}

	log.Println("Бот запущен...")
	telegramBot.Start()
}
