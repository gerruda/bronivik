package bot

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics структура для метрик Prometheus
type Metrics struct {
	MessagesProcessed    prometheus.Counter
	CommandsProcessed    prometheus.Counter
	CommandDuration      *prometheus.HistogramVec
	CalculationsTotal    *prometheus.CounterVec
	ErrorsTotal          prometheus.Counter
	UsersTotal           prometheus.Gauge
	UpdateProcessingTime prometheus.Histogram
	BookingsCreated      *prometheus.CounterVec
	BookingDuration      *prometheus.HistogramVec
}

// NewMetrics создает новые метрики
func NewMetrics() *Metrics {
	return &Metrics{
		// ... (existing metrics)
		UpdateProcessingTime: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "telegram_bot_update_processing_time_seconds",
			Help:    "Time spent processing updates",
			Buckets: prometheus.DefBuckets,
		}),

		BookingsCreated: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "telegram_bot_bookings_created_total",
			Help: "Total number of bookings created",
		}, []string{"item_name"}),

		BookingDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "telegram_bot_booking_duration_seconds",
			Help:    "Time spent creating a booking",
			Buckets: prometheus.DefBuckets,
		}, []string{"item_name"}),
	}
}
