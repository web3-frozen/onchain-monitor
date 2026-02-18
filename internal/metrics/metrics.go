package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// ── HTTP request metrics (RED method) ──────────────────────────────────

var (
	HTTPRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "onchain_monitor",
		Subsystem: "http",
		Name:      "requests_total",
		Help:      "Total number of HTTP requests.",
	}, []string{"method", "path", "status_code"})

	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "onchain_monitor",
		Subsystem: "http",
		Name:      "request_duration_seconds",
		Help:      "HTTP request latency in seconds.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method", "path"})

	HTTPRequestsInFlight = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "onchain_monitor",
		Subsystem: "http",
		Name:      "requests_in_flight",
		Help:      "Number of HTTP requests currently being processed.",
	})
)

// ── Polling / source metrics ───────────────────────────────────────────

var (
	PollTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "onchain_monitor",
		Subsystem: "poll",
		Name:      "total",
		Help:      "Total number of poll attempts per source.",
	}, []string{"source", "status"})

	PollDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "onchain_monitor",
		Subsystem: "poll",
		Name:      "duration_seconds",
		Help:      "Duration of poll fetch per source in seconds.",
		Buckets:   []float64{0.1, 0.25, 0.5, 1, 2, 5, 10, 30},
	}, []string{"source"})

	PollLastSuccess = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "onchain_monitor",
		Subsystem: "poll",
		Name:      "last_success_timestamp",
		Help:      "Unix timestamp of the last successful poll per source.",
	}, []string{"source"})

	SnapshotCount = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "onchain_monitor",
		Subsystem: "snapshot",
		Name:      "count",
		Help:      "Number of snapshots currently in memory per source.",
	}, []string{"source"})

	SnapshotAge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "onchain_monitor",
		Subsystem: "snapshot",
		Name:      "age_seconds",
		Help:      "Age of the latest snapshot in seconds per source.",
	}, []string{"source"})
)

// ── Alert delivery metrics ─────────────────────────────────────────────

var (
	AlertsSentTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "onchain_monitor",
		Subsystem: "alerts",
		Name:      "sent_total",
		Help:      "Total alerts successfully delivered.",
	}, []string{"source", "type"})

	AlertsFailedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "onchain_monitor",
		Subsystem: "alerts",
		Name:      "failed_total",
		Help:      "Total alert delivery failures.",
	}, []string{"source", "type"})

	AlertsDeduplicatedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "onchain_monitor",
		Subsystem: "alerts",
		Name:      "deduplicated_total",
		Help:      "Total alerts suppressed by deduplication.",
	}, []string{"source", "type"})
)

// ── Business metrics ───────────────────────────────────────────────────

var (
	MetricValue = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "onchain_monitor",
		Subsystem: "business",
		Name:      "metric_value",
		Help:      "Current value of a tracked business metric.",
	}, []string{"source", "metric_name"})

	SubscriptionsActive = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "onchain_monitor",
		Subsystem: "business",
		Name:      "subscriptions_active",
		Help:      "Number of active subscriptions per event.",
	}, []string{"event_name"})

	TelegramLinkedUsers = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "onchain_monitor",
		Subsystem: "business",
		Name:      "telegram_linked_users",
		Help:      "Total number of linked Telegram users.",
	})
)
