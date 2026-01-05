package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"bronivik/internal/bot"
	"bronivik/internal/config"
	"bronivik/internal/database"
	"bronivik/internal/events"
	"bronivik/internal/google"
	"bronivik/internal/logging"
	"bronivik/internal/models"
	"bronivik/internal/repository"
	"bronivik/internal/service"
	"bronivik/internal/worker"
	"github.com/redis/go-redis/v9"
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

	// Инициализация логгера
	baseLogger, closer, err := logging.New(cfg.Logging, cfg.App)
	if err != nil {
		log.Fatalf("init logger: %v", err)
	}
	if closer != nil {
		defer closer.Close()
	}
	logger := baseLogger.With().Str("component", "bot-main").Logger()

	if _, err := os.Stat("configs/items.yaml"); os.IsNotExist(err) {
		logger.Fatal().Msgf("Config file does not exist: %s", "configs/items.yaml")
	}

	// Загрузка позиций из отдельного файла
	itemsData, err := os.ReadFile("configs/items.yaml")
	if err != nil {
		logger.Fatal().Err(err).Msg("Ошибка чтения items.yaml")
	}

	var itemsConfig struct {
		Items []models.Item `yaml:"items"`
	}
	if err := yaml.Unmarshal(itemsData, &itemsConfig); err != nil {
		logger.Fatal().Err(err).Msg("Ошибка парсинга items.yaml")
	}

	if err := config.ValidateItems(itemsConfig.Items); err != nil {
		logger.Fatal().Err(err).Msg("Items validation failed")
	}

	// Создаем необходимые директории
	if cfg == nil {
		logger.Fatal().Msg("Cfg configuration is missing in config")
	}
	if err := os.MkdirAll(filepath.Dir(cfg.Database.Path), 0755); err != nil {
		logger.Fatal().Err(err).Msg("Ошибка создания директории для базы данных")
	}

	if err := os.MkdirAll(cfg.Exports.Path, 0755); err != nil {
		logger.Fatal().Err(err).Msg("Ошибка создания директории для экспорта")
	}

	// Инициализация базы данных
	db, err := database.NewDB(cfg.Database.Path)
	if err != nil {
		logger.Fatal().Err(err).Msg("Ошибка инициализации базы данных")
	}
	defer db.Close()

	// Устанавливаем items в базу данных
	db.SetItems(itemsConfig.Items)

	if cfg.Telegram.BotToken == "YOUR_BOT_TOKEN_HERE" {
		logger.Fatal().Msg("Задайте токен бота в config.yaml")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Инициализация Google Sheets через API Key
	var sheetsService *google.SheetsService
	if cfg.Google.GoogleCredentialsFile == "" || cfg.Google.UsersSpreadSheetId == "" || cfg.Google.BookingSpreadSheetId == "" {
		logger.Fatal().Msg("Нехватает переменных для подключения к Гуглу")
	}

	service, err := google.NewSimpleSheetsService(
		cfg.Google.GoogleCredentialsFile,
		cfg.Google.UsersSpreadSheetId,
		cfg.Google.BookingSpreadSheetId,
	)
	if err != nil {
		logger.Warn().Err(err).Msg("Failed to initialize Google Sheets service")
	}

	// Тестируем подключение
	if err := service.TestConnection(); err != nil {
		logger.Fatal().Err(err).Msg("Google Sheets connection test failed")
	} else {
		sheetsService = service
		logger.Info().Msg("Google Sheets service initialized successfully")
	}

	// Инициализация Redis
	var redisClient *redis.Client
	if cfg.Redis.Address != "" {
		redisClient = repository.NewRedisClient(cfg.Redis)
		if err := repository.Ping(ctx, redisClient); err != nil {
			logger.Warn().Err(err).Msg("Redis unavailable")
		}
	}

	// Инициализация сервиса состояний
	stateRepo := repository.NewRedisStateRepository(redisClient, 24*time.Hour)
	stateService := service.NewStateService(stateRepo, &logger)

	// Запускаем воркер синхронизации Google Sheets
	var sheetsWorker *worker.SheetsWorker
	if sheetsService != nil {
		retryPolicy := worker.RetryPolicy{MaxRetries: 5, InitialDelay: 2 * time.Second, MaxDelay: time.Minute, BackoffFactor: 2}
		sheetsWorker = worker.NewSheetsWorker(db, sheetsService, redisClient, retryPolicy, &logger)
		go sheetsWorker.Start(ctx)
	}

	eventBus := events.NewEventBus()
	subscribeBookingEvents(ctx, eventBus, db, sheetsWorker, &logger)

	// Создание и запуск бота
	telegramBot, err := bot.NewBot(cfg.Telegram.BotToken, cfg, itemsConfig.Items, db, stateService, sheetsService, sheetsWorker, eventBus, &logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Ошибка создания бота")
	}

	logger.Info().Msg("Бот запущен...")
	
	// Запускаем напоминания
	telegramBot.StartReminders(ctx)

	// Запускаем бота (блокирующий вызов)
	telegramBot.Start(ctx)

	logger.Info().Msg("Shutdown complete.")
}

func subscribeBookingEvents(ctx context.Context, bus *events.EventBus, db *database.DB, sheetsWorker *worker.SheetsWorker, logger *zerolog.Logger) {
	if bus == nil || sheetsWorker == nil || db == nil {
		return
	}

	decode := func(ev events.Event) (events.BookingEventPayload, error) {
		var payload events.BookingEventPayload
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			return payload, err
		}
		return payload, nil
	}

	upsertHandler := func(ev events.Event) error {
		payload, err := decode(ev)
		if err != nil {
			logger.Error().Err(err).Str("event", ev.Type).Msg("event bus: decode payload")
			return nil
		}

		booking, err := db.GetBooking(ctx, payload.BookingID)
		if err != nil {
			logger.Error().Err(err).Int64("booking_id", payload.BookingID).Msg("event bus: load booking")
			return nil
		}

		if err := sheetsWorker.EnqueueTask(ctx, worker.SheetTask{Type: worker.TaskUpsert, BookingID: booking.ID, Booking: booking}); err != nil {
			logger.Error().Err(err).Int64("booking_id", booking.ID).Msg("event bus: enqueue upsert")
		}
		return nil
	}

	statusHandler := func(ev events.Event) error {
		payload, err := decode(ev)
		if err != nil {
			logger.Error().Err(err).Str("event", ev.Type).Msg("event bus: decode payload")
			return nil
		}

		status := payload.Status
		if status == "" {
			booking, err := db.GetBooking(ctx, payload.BookingID)
			if err == nil {
				status = booking.Status
			}
		}

		if status == "" {
			logger.Error().Int64("booking_id", payload.BookingID).Msg("event bus: missing status")
			return nil
		}

		if err := sheetsWorker.EnqueueTask(ctx, worker.SheetTask{Type: worker.TaskUpdateStatus, BookingID: payload.BookingID, Status: status}); err != nil {
			logger.Error().Err(err).Int64("booking_id", payload.BookingID).Msg("event bus: enqueue status")
		}
		return nil
	}

	bus.Subscribe(events.EventBookingCreated, upsertHandler)
	bus.Subscribe(events.EventBookingItemChange, upsertHandler)
	bus.Subscribe(events.EventBookingConfirmed, statusHandler)
	bus.Subscribe(events.EventBookingCancelled, statusHandler)
	bus.Subscribe(events.EventBookingCompleted, statusHandler)
}
