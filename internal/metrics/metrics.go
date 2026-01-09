package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	once sync.Once

	httpRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "bronivik_jr",
			Name:      "http_requests_total",
			Help:      "HTTP requests by endpoint.",
		},
		[]string{"endpoint"},
	)
)

// Register registers Prometheus metrics. Safe to call multiple times.
func Register() {
	once.Do(func() {
		prometheus.MustRegister(httpRequests)
	})
}

// IncHTTP increments the counter for an endpoint label.
func IncHTTP(endpoint string) {
	httpRequests.WithLabelValues(endpoint).Inc()
}
