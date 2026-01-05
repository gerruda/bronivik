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

	"bronivik/internal/api"
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

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
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
	db, err := database.NewDB(cfg.Database.Path, &logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Ошибка инициализации базы данных")
	}
	defer db.Close()

	// Синхронизируем items с базой данных
	if err := db.SyncItems(context.Background(), itemsConfig.Items); err != nil {
		logger.Error().Err(err).Msg("Ошибка синхронизации позиций")
	}

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

	sheetsSvc, err := google.NewSimpleSheetsService(
		cfg.Google.GoogleCredentialsFile,
		cfg.Google.UsersSpreadSheetId,
		cfg.Google.BookingSpreadSheetId,
	)
	if err != nil {
		logger.Warn().Err(err).Msg("Failed to initialize Google Sheets service")
	}

	// Тестируем подключение
	if err := sheetsSvc.TestConnection(ctx); err != nil {
		logger.Fatal().Err(err).Msg("Google Sheets connection test failed")
	} else {
		sheetsService = sheetsSvc
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
	primaryRepo := repository.NewRedisStateRepository(redisClient, time.Duration(models.DefaultRedisTTL)*time.Second)
	fallbackRepo := repository.NewMemoryStateRepository(time.Duration(models.DefaultRedisTTL) * time.Second)
	stateRepo := repository.NewFailoverStateRepository(primaryRepo, fallbackRepo, &logger)
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

	// Инициализация бизнес-сервисов
	bookingService := service.NewBookingService(db, eventBus, sheetsWorker, cfg.Bot.MaxBookingDays, cfg.Bot.MinBookingAdvance, &logger)
	userService := service.NewUserService(db, cfg, &logger)
	itemService := service.NewItemService(db, &logger)
	metrics := bot.NewMetrics()

	// Инициализация API сервера
	if cfg.API.Enabled {
		apiServer := api.NewHTTPServer(cfg.API, db, &logger)
		go func() {
			if err := apiServer.Start(); err != nil {
				logger.Error().Err(err).Msg("API server error")
			}
		}()
		defer apiServer.Shutdown(context.Background())
	}

	// Инициализация сервиса бэкапов
	if cfg.Backup.Enabled {
		backupService := database.NewBackupService(cfg.Database.Path, cfg.Backup, &logger)
		go backupService.Start(ctx)
	}

	// Создание и запуск бота
	botAPI, err := tgbotapi.NewBotAPI(cfg.Telegram.BotToken)
	if err != nil {
		logger.Fatal().Err(err).Msg("Ошибка создания BotAPI")
	}
	botWrapper := bot.NewBotWrapper(botAPI)
	tgService := service.NewTelegramService(botWrapper)

	telegramBot, err := bot.NewBot(tgService, cfg, stateService, sheetsService, sheetsWorker, eventBus, bookingService, userService, itemService, metrics, &logger)
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

		if err := sheetsWorker.EnqueueTask(ctx, "upsert", booking.ID, booking, ""); err != nil {
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

		if err := sheetsWorker.EnqueueTask(ctx, "update_status", payload.BookingID, nil, status); err != nil {
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
