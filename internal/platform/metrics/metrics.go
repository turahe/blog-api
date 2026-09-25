// Package metrics exposes Prometheus metrics for HTTP traffic, the database pool,
// and the Go runtime on a dedicated registry.
package metrics

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const namespace = "blog"

// Metrics owns the registry and the application collectors.
type Metrics struct {
	registry *prometheus.Registry
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	inflight prometheus.Gauge
	dropped  prometheus.Counter

	outboxPublished prometheus.Counter
	outboxFailed    *prometheus.CounterVec
	outboxPending   prometheus.Gauge
	outboxParked    prometheus.Gauge
	outboxLag       prometheus.Gauge
}

// New registers runtime, process, build, and (when db is non-nil) sql.DB pool collectors.
func New(db *sql.DB, version string) *Metrics {
	registry := prometheus.NewRegistry()

	m := &Metrics{
		registry: registry,
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "http_requests_total",
			Help:      "HTTP requests by method, route template, and status code.",
		}, []string{"method", "route", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "http_request_duration_seconds",
			Help:      "HTTP request latency by method and route template.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"method", "route"}),
		inflight: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "http_requests_in_flight",
			Help:      "HTTP requests currently being served.",
		}),
		dropped: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "audit_entries_dropped_total",
			Help:      "Audit entries lost to a full queue or a failed insert.",
		}),
		outboxPublished: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "outbox_published_total",
			Help:      "Outbox events published to the message broker.",
		}),
		outboxFailed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "outbox_publish_failures_total",
			Help:      "Failed outbox publishes; terminal=true when the event was parked.",
		}, []string{"terminal"}),
		outboxPending: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "outbox_pending",
			Help:      "Outbox events waiting to be published.",
		}),
		outboxParked: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "outbox_failed",
			Help:      "Outbox events parked after their last attempt.",
		}),
		outboxLag: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "outbox_lag_seconds",
			Help:      "Age of the oldest outbox event waiting to be published; 0 when none.",
		}),
	}

	build := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace:   namespace,
		Name:        "build_info",
		Help:        "Build metadata; always 1.",
		ConstLabels: prometheus.Labels{"version": version},
	})
	build.Set(1)

	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.requests, m.duration, m.inflight, m.dropped, build,
		m.outboxPublished, m.outboxFailed, m.outboxPending, m.outboxParked, m.outboxLag,
	)

	if db != nil {
		registry.MustRegister(collectors.NewDBStatsCollector(db, namespace))
	}

	return m
}

// Handler serves the registry in the Prometheus exposition format.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{Registry: m.registry})
}

// Registry returns the registry for extra collectors.
func (m *Metrics) Registry() *prometheus.Registry {
	return m.registry
}

// StartRequest marks a request in flight; call the returned func when it ends.
func (m *Metrics) StartRequest() func() {
	m.inflight.Inc()
	return m.inflight.Dec
}

// ObserveHTTP records one finished request. route must be a bounded value such
// as the matched route template, never the raw path.
func (m *Metrics) ObserveHTTP(method, route string, status int, elapsed time.Duration) {
	m.requests.WithLabelValues(method, route, strconv.Itoa(status)).Inc()
	m.duration.WithLabelValues(method, route).Observe(elapsed.Seconds())
}

// AuditDropped counts n audit entries that were not stored.
func (m *Metrics) AuditDropped(n int) {
	m.dropped.Add(float64(n))
}

// OutboxPublished counts one relayed event.
func (m *Metrics) OutboxPublished() {
	m.outboxPublished.Inc()
}

// OutboxPublishFailed counts one failed publish.
func (m *Metrics) OutboxPublishFailed(terminal bool) {
	m.outboxFailed.WithLabelValues(strconv.FormatBool(terminal)).Inc()
}

// OutboxBacklog records the outbox backlog sampled at now.
func (m *Metrics) OutboxBacklog(pending, failed int64, oldestPendingAt *time.Time, now time.Time) {
	m.outboxPending.Set(float64(pending))
	m.outboxParked.Set(float64(failed))

	lag := 0.0
	if oldestPendingAt != nil {
		lag = max(now.Sub(*oldestPendingAt).Seconds(), 0)
	}

	m.outboxLag.Set(lag)
}

// NewServer returns the metrics HTTP server for addr, serving /metrics only.
func (m *Metrics) NewServer(addr string) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", m.Handler())

	return &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
}
