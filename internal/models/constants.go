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
)
