package bot

import (
	"context"
	"os"
	"time"

	"bronivik/internal/config"
	"bronivik/internal/database"
	"bronivik/internal/events"
	"bronivik/internal/google"
	"bronivik/internal/models"
	"bronivik/internal/service"
	"bronivik/internal/worker"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type Bot struct {
	bot           *tgbotapi.BotAPI
	config        *config.Config
	items         []models.Item
	db            *database.DB
	stateService  *service.StateService
	sheetsService *google.SheetsService
	sheetsWorker  *worker.SheetsWorker
	eventBus      *events.EventBus
	logger        *zerolog.Logger
}

func NewBot(token string, config *config.Config, items []models.Item, db *database.DB, stateService *service.StateService, googleService *google.SheetsService, sheetsWorker *worker.SheetsWorker, eventBus *events.EventBus, logger *zerolog.Logger) (*Bot, error) {
	botAPI, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, err
	}

	if eventBus == nil {
		eventBus = events.NewEventBus()
	}

	if logger == nil {
		l := zerolog.New(os.Stdout).With().Timestamp().Logger()
		logger = &l
	}

	return &Bot{
		bot:           botAPI,
		config:        config,
		items:         items,
		db:            db,
		stateService:  stateService,
		sheetsService: googleService,
		sheetsWorker:  sheetsWorker,
		eventBus:      eventBus,
		logger:        logger,
	}, nil
}

const (
	StateMainMenu            = "main_menu"
	StateSelectItem          = "select_item"
	StateSelectDate          = "select_date"
	StateViewSchedule        = "view_schedule"
	StatePersonalData        = "personal_data"
	StateEnterName           = "enter_name"
	StatePhoneNumber         = "phone_number"
	StateConfirmation        = "confirmation"
	StateWaitingDate         = "waiting_date"
	StateWaitingSpecificDate = "waiting_specific_date"
)

func (b *Bot) Start(ctx context.Context) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.bot.GetUpdatesChan(u)

	b.logger.Info().Str("username", b.bot.Self.UserName).Msg("Authorized on account")

	for {
		select {
		case <-ctx.Done():
			b.logger.Info().Msg("Bot stopping...")
			return
		case update, ok := <-updates:
			if !ok {
				return
			}
			// Создаем контекст для обработки каждого обновления
			updateCtx, cancel := context.WithTimeout(ctx, 30*time.Second)

			requestID := uuid.New().String()
			l := b.logger.With().Str("request_id", requestID).Logger()
			updateCtx = l.WithContext(updateCtx)
			
			if update.CallbackQuery != nil {
				b.handleCallbackQuery(updateCtx, update)
				cancel()
				continue
			}

			if update.Message == nil {
				cancel()
				continue
			}

			if b.isBlacklisted(update.Message.From.ID) {
				cancel()
				continue
			}

			b.handleMessage(updateCtx, update)
			cancel()
		}
	}
}
