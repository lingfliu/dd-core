package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	SyncRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dd_sync_requests_total",
			Help: "Total number of sync requests.",
		},
		[]string{"resource", "status"},
	)

	SyncLatencyMs = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "dd_sync_latency_ms",
			Help:    "Sync request latency in milliseconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"resource"},
	)

	AsyncPublishedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dd_async_published_total",
			Help: "Total number of async messages published.",
		},
		[]string{"resource", "status"},
	)

	PeerActiveCount = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "dd_peer_active_count",
			Help: "Number of active peers.",
		},
	)

	PeerStaleCount = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "dd_peer_stale_count",
			Help: "Number of stale peers.",
		},
	)

	TimeoutTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "dd_timeout_total",
			Help: "Total number of sync timeouts.",
		},
	)

	AclDeniedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "dd_acl_denied_total",
			Help: "Total number of ACL denied requests.",
		},
	)
)
