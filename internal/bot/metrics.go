package bot

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics структура для метрик Prometheus
type Metrics struct {
	MessagesProcessed    prometheus.Counter
	CommandsProcessed    prometheus.Counter
	ErrorsTotal          prometheus.Counter
	UsersTotal           prometheus.Gauge
	UpdateProcessingTime prometheus.Histogram
	BookingsCreated      *prometheus.CounterVec
	BookingDuration      *prometheus.HistogramVec
}

// NewMetrics создает новые метрики
func NewMetrics() *Metrics {
	return &Metrics{
		MessagesProcessed: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: "bronivik_jr",
			Subsystem: "bot",
			Name:      "messages_processed_total",
			Help:      "Total number of messages processed",
		}),

		CommandsProcessed: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: "bronivik_jr",
			Subsystem: "bot",
			Name:      "commands_processed_total",
			Help:      "Total number of commands processed",
		}),

		ErrorsTotal: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: "bronivik_jr",
			Subsystem: "bot",
			Name:      "errors_total",
			Help:      "Total number of errors encountered",
		}),

		UsersTotal: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: "bronivik_jr",
			Subsystem: "bot",
			Name:      "users_active_total",
			Help:      "Total number of active users",
		}),

		UpdateProcessingTime: promauto.NewHistogram(prometheus.HistogramOpts{
			Namespace: "bronivik_jr",
			Subsystem: "bot",
			Name:      "update_processing_time_seconds",
			Help:      "Time spent processing updates",
			Buckets:   prometheus.DefBuckets,
		}),

		BookingsCreated: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "bronivik_jr",
			Subsystem: "bot",
			Name:      "bookings_created_total",
			Help:      "Total number of bookings created",
		}, []string{"item_name"}),

		BookingDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "bronivik_jr",
			Subsystem: "bot",
			Name:      "booking_duration_seconds",
			Help:      "Time spent creating a booking",
			Buckets:   prometheus.DefBuckets,
		}, []string{"item_name"}),
	}
}

// startMetricsUpdater periodically updates gauge metrics.
func (b *Bot) startMetricsUpdater(ctx context.Context) {
	if b.metrics == nil {
		return
	}

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	// Initial update
	b.updateGaugeMetrics(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.updateGaugeMetrics(ctx)
		}
	}
}

func (b *Bot) updateGaugeMetrics(ctx context.Context) {
	// Update active users count (e.g., active in last 30 days)
	users, err := b.userService.GetActiveUsers(ctx, 30)
	if err == nil {
		b.metrics.UsersTotal.Set(float64(len(users)))
	}
}
