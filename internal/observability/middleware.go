package observability

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dd_http_requests_total",
			Help: "Total HTTP requests.",
		},
		[]string{"method", "path", "status"},
	)

	httpRequestDurationMs = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "dd_http_request_duration_ms",
			Help:    "HTTP request duration in milliseconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)
)

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func RequestLoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(rw, r)

		duration := time.Since(start)
		statusClass := fmtStatusClass(rw.statusCode)

		httpRequestsTotal.WithLabelValues(r.Method, r.URL.Path, statusClass).Inc()
		httpRequestDurationMs.WithLabelValues(r.Method, r.URL.Path).Observe(float64(duration.Milliseconds()))

		slog.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.statusCode,
			"duration_ms", duration.Milliseconds(),
		)
	})
}

func fmtStatusClass(code int) string {
	switch {
	case code >= 500:
		return "5xx"
	case code >= 400:
		return "4xx"
	case code >= 300:
		return "3xx"
	case code >= 200:
		return "2xx"
	default:
		return "1xx"
	}
}
