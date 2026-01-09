package bot

import (
	"context"
	"os"
	"time"

	"bronivik/internal/config"
	"bronivik/internal/domain"
	"bronivik/internal/events"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

const (
	btnCancel               = "❌ Отмена"
	btnBack                 = "⬅️ Назад"
	btnCreateBooking        = "📋 СОЗДАТЬ ЗАЯВКУ"
	btnMyBookings           = "📊 Мои заявки"
	btnManagerContacts      = "📞 Контакты менеджеров"
	btnAvailableItems       = "💼 Ассортимент"
	btnViewSchedule         = "📅 Посмотреть расписание"
	btnMonthSchedule        = "📅 30 дней"
	btnPickDate             = "🗓 Выбрать дату"
	btnBackToItems          = "⬅️ Назад к выбору аппарата"
	btnCreateForItem        = "📋 СОЗДАТЬ ЗАЯВКУ НА ЭТОТ АППАРАТ"
	btnAllBookings          = "👨‍💼 Все заявки"
	btnCreateBookingManager = "➕ Создать заявку (Менеджер)"
	btnSyncBookings         = "🔄 Синхронизировать бронирования (Google Sheets)"
	btnSyncSchedule         = "📅 Синхронизировать расписание (Google Sheets)"
	btnConfirmCreate        = "✅ Подтвердить создание"

	statusSuccess = "✅"
	statusPending = "⏳"
	statusError   = "❌"
	typeSingle    = "single"
)

// Bot represents the Telegram bot instance and its dependencies.
type Bot struct {
	tgService      domain.TelegramService
	config         *config.Config
	stateService   domain.StateManager
	sheetsService  domain.SheetsWriter
	sheetsWorker   domain.SyncWorker
	eventBus       domain.EventPublisher
	bookingService domain.BookingService
	userService    domain.UserService
	itemService    domain.ItemService
	metrics        *Metrics
	logger         *zerolog.Logger
}

// NewBot creates a new instance of the Telegram bot.
func NewBot(
	tgService domain.TelegramService,
	config *config.Config,
	stateService domain.StateManager,
	googleService domain.SheetsWriter,
	sheetsWorker domain.SyncWorker,
	eventBus domain.EventPublisher,
	bookingService domain.BookingService,
	userService domain.UserService,
	itemService domain.ItemService,
	metrics *Metrics,
	logger *zerolog.Logger,
) (*Bot, error) {
	if eventBus == nil {
		eventBus = events.NewEventBus()
	}

	if logger == nil {
		l := zerolog.New(os.Stdout).With().Timestamp().Logger()
		logger = &l
	}

	return &Bot{
		tgService:      tgService,
		config:         config,
		stateService:   stateService,
		sheetsService:  googleService,
		sheetsWorker:   sheetsWorker,
		eventBus:       eventBus,
		bookingService: bookingService,
		userService:    userService,
		itemService:    itemService,
		metrics:        metrics,
		logger:         logger,
	}, nil
}

// Start begins the bot's update polling loop.
func (b *Bot) Start(ctx context.Context) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.tgService.GetUpdatesChan(u)

	b.logger.Info().Str("username", b.tgService.GetSelf().UserName).Msg("Authorized on account")

	// Start metrics updater
	go b.startMetricsUpdater(ctx)

	for {
		select {
		case <-ctx.Done():
			b.logger.Info().Msg("Bot stopping...")
			return
		case update, ok := <-updates:
			if !ok {
				return
			}
			b.processUpdate(ctx, &update)
		}
	}
}

// processUpdate handles a single Telegram update.
func (b *Bot) processUpdate(ctx context.Context, update *tgbotapi.Update) {
	start := time.Now()
	defer func() {
		if b.metrics != nil {
			b.metrics.UpdateProcessingTime.Observe(time.Since(start).Seconds())
		}
	}()

	// Создаем контекст для обработки каждого обновления
	updateCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	requestID := uuid.New().String()
	l := b.logger.With().Str("request_id", requestID).Logger()
	updateCtx = l.WithContext(updateCtx)

	b.withRecovery(func() {
		var userID int64
		if update.Message != nil {
			userID = update.Message.From.ID
		} else if update.CallbackQuery != nil {
			userID = update.CallbackQuery.From.ID
		}

		if userID == 0 {
			return
		}

		// Track activity
		b.trackActivity(userID)

		if !b.isManager(userID) {
			window := time.Duration(b.config.Bot.RateLimitWindow) * time.Second
			allowed, err := b.stateService.CheckRateLimit(updateCtx, userID, b.config.Bot.RateLimitMessages, window)
			if err != nil {
				b.logger.Error().Err(err).Int64("user_id", userID).Msg("Rate limit check failed")
			} else if !allowed {
				b.logger.Warn().Int64("user_id", userID).Msg("Rate limit exceeded")
				if update.Message != nil {
					b.sendMessage(update.Message.Chat.ID, "⚠️ Вы отправляете сообщения слишком часто. Пожалуйста, подождите немного.")
				} else if update.CallbackQuery != nil {
					callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "⚠️ Слишком много запросов. Подождите немного.")
					callbackConfig.ShowAlert = true
					_, _ = b.tgService.Request(callbackConfig)
				}
				return
			}
		}

		if update.CallbackQuery != nil {
			b.handleCallbackQuery(updateCtx, update)
			return
		}

		if update.Message == nil {
			return
		}

		if b.isBlacklisted(update.Message.From.ID) {
			return
		}

		b.handleMessage(updateCtx, update)
	})
}
