package main

import (
	"context"
	"encoding/json"
	"io"
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
	if err := run(); err != nil {
		log.Fatalf("Fatal error: %v", err)
	}
}

func run() error {
	cfg, items, logger, closer, loadErr := loadConfigAndLogger()
	if loadErr != nil {
		return loadErr
	}
	if closer != nil {
		defer (func(c io.Closer) { _ = c.Close() })(closer)
	}

	if err := prepareDirectories(cfg, &logger); err != nil {
		return err
	}

	db, err := initDatabase(cfg, items, &logger)
	if err != nil {
		return err
	}
	defer db.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sheetsService, err := initGoogleSheets(ctx, cfg, &logger)
	if err != nil {
		return err
	}

	redisClient, stateService := initStateService(ctx, cfg, &logger)

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

	if cfg.API.Enabled {
		apiServer := api.NewHTTPServer(&cfg.API, db, redisClient, sheetsService, &logger)
		go func() {
			if err := apiServer.Start(); err != nil {
				logger.Error().Err(err).Msg("API server error")
			}
		}()
		defer func() {
			_ = apiServer.Shutdown(context.Background())
		}()
	}

	if cfg.Backup.Enabled {
		backupService := database.NewBackupService(cfg.Database.Path, cfg.Backup, &logger)
		go backupService.Start(ctx)
	}

	return startBot(ctx, cfg, stateService, sheetsService, sheetsWorker, eventBus, bookingService, userService, itemService, metrics, &logger)
}

func loadConfigAndLogger() (*config.Config, []models.Item, zerolog.Logger, io.Closer, error) {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "configs/config.yaml"
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, nil, zerolog.Logger{}, nil, err
	}

	baseLogger, closer, err := logging.New(cfg.Logging, cfg.App)
	if err != nil {
		return nil, nil, zerolog.Logger{}, nil, err
	}
	logger := baseLogger.With().Str("component", "bot-main").Logger()

	itemsPath := os.Getenv("ITEMS_PATH")
	if itemsPath == "" {
		itemsPath = "configs/items.yaml"
	}
	itemsData, err := os.ReadFile(itemsPath)
	if err != nil {
		logger.Error().Err(err).Msgf("Ошибка чтения %s", itemsPath)
		return nil, nil, zerolog.Logger{}, closer, err
	}

	var itemsConfig struct {
		Items []models.Item `yaml:"items"`
	}
	if err := yaml.Unmarshal(itemsData, &itemsConfig); err != nil {
		logger.Error().Err(err).Msg("Ошибка парсинга items.yaml")
		return nil, nil, zerolog.Logger{}, closer, err
	}

	if err := config.ValidateItems(itemsConfig.Items); err != nil {
		logger.Error().Err(err).Msg("Items validation failed")
		return nil, nil, zerolog.Logger{}, closer, err
	}

	return cfg, itemsConfig.Items, logger, closer, nil
}

func prepareDirectories(cfg *config.Config, logger *zerolog.Logger) error {
	if cfg == nil {
		return os.ErrInvalid
	}
	if err := os.MkdirAll(filepath.Dir(cfg.Database.Path), 0o755); err != nil {
		logger.Error().Err(err).Msg("Ошибка создания директории для базы данных")
		return err
	}
	if err := os.MkdirAll(cfg.Exports.Path, 0o755); err != nil {
		logger.Error().Err(err).Msg("Ошибка создания директории для экспорта")
		return err
	}
	return nil
}

func initDatabase(cfg *config.Config, items []models.Item, logger *zerolog.Logger) (*database.DB, error) {
	db, err := database.NewDB(cfg.Database.Path, logger)
	if err != nil {
		logger.Error().Err(err).Msg("Ошибка инициализации базы данных")
		return nil, err
	}

	if err := db.SyncItems(context.Background(), items); err != nil {
		logger.Error().Err(err).Msg("Ошибка синхронизации позиций")
	}
	return db, nil
}

func initGoogleSheets(ctx context.Context, cfg *config.Config, logger *zerolog.Logger) (*google.SheetsService, error) {
	if cfg.Google.GoogleCredentialsFile == "" || cfg.Google.UsersSpreadSheetID == "" || cfg.Google.BookingSpreadSheetID == "" {
		logger.Error().Msg("Нехватает переменных для подключения к Гуглу")
		return nil, os.ErrInvalid
	}

	sheetsSvc, err := google.NewSimpleSheetsService(
		cfg.Google.GoogleCredentialsFile,
		cfg.Google.UsersSpreadSheetID,
		cfg.Google.BookingSpreadSheetID,
	)
	if err != nil {
		logger.Warn().Err(err).Msg("Failed to initialize Google Sheets service")
		return nil, err
	}

	if err := sheetsSvc.TestConnection(ctx); err != nil {
		logger.Error().Err(err).Msg("Google Sheets connection test failed")
		return nil, err
	}

	logger.Info().Msg("Google Sheets service initialized successfully")
	return sheetsSvc, nil
}

func initStateService(ctx context.Context, cfg *config.Config, logger *zerolog.Logger) (*redis.Client, *service.StateService) {
	var redisClient *redis.Client
	if cfg.Redis.Address != "" {
		redisClient = repository.NewRedisClient(cfg.Redis)
		if errPing := repository.Ping(ctx, redisClient); errPing != nil {
			logger.Warn().Err(errPing).Msg("Redis unavailable")
		}
	}

	primaryRepo := repository.NewRedisStateRepository(redisClient, time.Duration(models.DefaultRedisTTL)*time.Second)
	fallbackRepo := repository.NewMemoryStateRepository(time.Duration(models.DefaultRedisTTL) * time.Second)
	stateRepo := repository.NewFailoverStateRepository(primaryRepo, fallbackRepo, logger)
	return redisClient, service.NewStateService(stateRepo, logger)
}

func startBot(
	ctx context.Context,
	cfg *config.Config,
	stateService *service.StateService,
	sheetsService *google.SheetsService,
	sheetsWorker *worker.SheetsWorker,
	eventBus *events.EventBus,
	bookingService *service.BookingService,
	userService *service.UserService,
	itemService *service.ItemService,
	metrics *bot.Metrics,
	logger *zerolog.Logger,
) error {
	if cfg.Telegram.BotToken == "YOUR_BOT_TOKEN_HERE" {
		logger.Error().Msg("Задайте токен бота в config.yaml")
		return os.ErrInvalid
	}

	botAPI, err := tgbotapi.NewBotAPI(cfg.Telegram.BotToken)
	if err != nil {
		logger.Error().Err(err).Msg("Ошибка создания BotAPI")
		return err
	}

	botWrapper := bot.NewBotWrapper(botAPI)
	tgService := service.NewTelegramService(botWrapper)

	telegramBot, err := bot.NewBot(
		tgService, cfg, stateService, sheetsService,
		sheetsWorker, eventBus, bookingService, userService,
		itemService, metrics, logger,
	)
	if err != nil {
		logger.Error().Err(err).Msg("Ошибка создания бота")
		return err
	}

	logger.Info().Msg("Бот запущен...")
	telegramBot.StartReminders(ctx)
	telegramBot.Start(ctx)

	logger.Info().Msg("Shutdown complete.")
	return nil
}

func subscribeBookingEvents(
	ctx context.Context,
	bus *events.EventBus,
	db *database.DB,
	sheetsWorker *worker.SheetsWorker,
	logger *zerolog.Logger,
) {
	if bus == nil || sheetsWorker == nil || db == nil {
		return
	}

	decode := func(ev *events.Event) (events.BookingEventPayload, error) {
		var payload events.BookingEventPayload
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			return payload, err
		}
		return payload, nil
	}

	upsertHandler := func(ev *events.Event) error {
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

	statusHandler := func(ev *events.Event) error {
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
	bus.Subscribe(events.EventBookingCanceled, statusHandler)
	bus.Subscribe(events.EventBookingCompleted, statusHandler)
}
