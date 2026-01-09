package models

const (
	StatusPending   = "pending"
	StatusConfirmed = "confirmed"
	StatusCanceled  = "canceled"
	StatusChanged   = "changed"
	StatusCompleted = "completed"
)

const (
	ParseModeMarkdown = "Markdown"
	ParseModeHTML     = "HTML"
)

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

	// Manager States
	StateManagerWaitingClientName    = "manager_waiting_client_name"
	StateManagerWaitingClientPhone   = "manager_waiting_client_phone"
	StateManagerWaitingItemSelection = "manager_waiting_item_selection"
	StateManagerWaitingDateType      = "manager_waiting_date_type"
	StateManagerWaitingSingleDate    = "manager_waiting_single_date"
	StateManagerWaitingStartDate     = "manager_waiting_start_date"
	StateManagerWaitingEndDate       = "manager_waiting_end_date"
	StateManagerWaitingComment       = "manager_waiting_comment"
	StateManagerConfirmBooking       = "manager_confirm_booking"
)

const (
	// DefaultRedisTTL время жизни состояния пользователя в Redis
	DefaultRedisTTL = 24 * 60 * 60 // 24 часа в секундах

	// ReminderHour час, в который отправляются напоминания
	ReminderHour = 9

	// DefaultExportRangeMonths количество месяцев для экспорта по умолчанию
	DefaultExportRangeMonthsBefore = 1
	DefaultExportRangeMonthsAfter  = 2

	// WorkerQueueSize размер очереди воркера
	WorkerQueueSize = 1000

	// DefaultPaginationSize размер пагинации по умолчанию
	DefaultPaginationSize = 8

	// DefaultBookingsPaginationSize размер пагинации для списка заявок
	DefaultBookingsPaginationSize = 5

	// RateLimitMessages количество сообщений в окне
	RateLimitMessages = 20

	// RateLimitWindow окно ограничения частоты сообщений
	RateLimitWindow = 60 // 1 минута в секундах

	// ItemsCacheTTL время жизни кэша предметов в памяти
	ItemsCacheTTL = 30 * 60 // 30 минут в секундах

	// SheetsCacheTTL время жизни кэша строк Google Sheets
	SheetsCacheTTL = 60 * 60 // 1 час в секундах
)
