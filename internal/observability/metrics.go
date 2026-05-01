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

	AsyncBatchRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dd_async_batch_requests_total",
			Help: "Total number of async batch publish requests.",
		},
		[]string{"status"},
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

	BridgeRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dd_bridge_requests_total",
			Help: "Total number of bridge requests by protocol/mode/status.",
		},
		[]string{"protocol", "mode", "status"},
	)

	BridgeLatencyMs = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "dd_bridge_latency_ms",
			Help:    "Bridge request latency in milliseconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"protocol", "mode"},
	)

	BridgeRetriesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dd_bridge_retries_total",
			Help: "Total number of bridge retry attempts.",
		},
		[]string{"protocol"},
	)

	ResourceMetaValidateTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dd_resource_meta_validate_total",
			Help: "Total number of resource meta schema validations.",
		},
		[]string{"resource", "result"},
	)

	RouteDecisionTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dd_route_decision_total",
			Help: "Total number of route decisions by mode.",
		},
		[]string{"mode"},
	)

	ProtocolResourceQueryTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dd_protocol_resource_query_total",
			Help: "Total number of protocol resource query requests.",
		},
		[]string{"protocol", "mode"},
	)
)
