package models

const (
	StatusPending   = "pending"
	StatusConfirmed = "confirmed"
	StatusCancelled = "cancelled"
	StatusChanged   = "changed"
	StatusCompleted = "completed"
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
