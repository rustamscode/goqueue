// Package metrics provides Prometheus metrics for GoQueue.
package metrics

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rustamscode/goqueue/pkg/broker"
	"github.com/rustamscode/goqueue/pkg/goqueue"
)

const (
	namespace = "goqueue"
)

// Metrics holds all GoQueue Prometheus metrics.
type Metrics struct {
	// Task metrics
	TasksProcessed *prometheus.CounterVec
	TasksFailed    *prometheus.CounterVec
	TasksRetried   *prometheus.CounterVec
	TasksEnqueued  *prometheus.CounterVec
	TaskDuration   *prometheus.HistogramVec

	// Queue metrics
	QueueDepth     *prometheus.GaugeVec
	QueueActive    *prometheus.GaugeVec
	DLQDepth       *prometheus.GaugeVec
	ScheduledCount prometheus.Gauge

	// Worker metrics
	WorkersActive prometheus.Gauge
	WorkersTotal  prometheus.Gauge

	// System metrics
	BrokerConnected prometheus.Gauge

	broker   broker.Broker
	registry *prometheus.Registry
	once     sync.Once
}

// NewMetrics creates a new Metrics instance with the given broker.
func NewMetrics(b broker.Broker) *Metrics {
	m := &Metrics{
		broker: b,
	}
	m.init()
	return m
}

// init initializes all metrics.
func (m *Metrics) init() {
	m.once.Do(func() {
		m.registry = prometheus.NewRegistry()

		// Task counters
		m.TasksProcessed = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "tasks_processed_total",
				Help:      "Total number of tasks processed",
			},
			[]string{"queue", "status"},
		)

		m.TasksFailed = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "tasks_failed_total",
				Help:      "Total number of failed tasks",
			},
			[]string{"queue"},
		)

		m.TasksRetried = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "tasks_retried_total",
				Help:      "Total number of task retries",
			},
			[]string{"queue"},
		)

		m.TasksEnqueued = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "tasks_enqueued_total",
				Help:      "Total number of tasks enqueued",
			},
			[]string{"queue"},
		)

		m.TaskDuration = prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "task_duration_seconds",
				Help:      "Task processing duration in seconds",
				Buckets:   prometheus.ExponentialBuckets(0.001, 2, 15), // 1ms to ~16s
			},
			[]string{"queue", "task_type"},
		)

		// Queue gauges
		m.QueueDepth = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "queue_depth",
				Help:      "Number of pending tasks in queue",
			},
			[]string{"queue"},
		)

		m.QueueActive = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "queue_active",
				Help:      "Number of active tasks in queue",
			},
			[]string{"queue"},
		)

		m.DLQDepth = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "dlq_depth",
				Help:      "Number of tasks in dead letter queue",
			},
			[]string{"queue"},
		)

		m.ScheduledCount = prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "scheduled_count",
				Help:      "Number of scheduled tasks",
			},
		)

		// Worker gauges
		m.WorkersActive = prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "workers_active",
				Help:      "Number of workers currently processing tasks",
			},
		)

		m.WorkersTotal = prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "workers_total",
				Help:      "Total number of worker goroutines",
			},
		)

		// System gauges
		m.BrokerConnected = prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "broker_connected",
				Help:      "Whether the broker connection is healthy (1=connected, 0=disconnected)",
			},
		)

		// Register all metrics
		m.registry.MustRegister(
			m.TasksProcessed,
			m.TasksFailed,
			m.TasksRetried,
			m.TasksEnqueued,
			m.TaskDuration,
			m.QueueDepth,
			m.QueueActive,
			m.DLQDepth,
			m.ScheduledCount,
			m.WorkersActive,
			m.WorkersTotal,
			m.BrokerConnected,
		)
	})
}

// Handler returns an HTTP handler for the /metrics endpoint.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

// Registry returns the Prometheus registry.
func (m *Metrics) Registry() *prometheus.Registry {
	return m.registry
}

// RecordTaskProcessed records a successfully processed task.
func (m *Metrics) RecordTaskProcessed(queue string) {
	m.TasksProcessed.WithLabelValues(queue, "success").Inc()
}

// RecordTaskFailed records a failed task.
func (m *Metrics) RecordTaskFailed(queue string) {
	m.TasksProcessed.WithLabelValues(queue, "failed").Inc()
	m.TasksFailed.WithLabelValues(queue).Inc()
}

// RecordTaskRetried records a task retry.
func (m *Metrics) RecordTaskRetried(queue string) {
	m.TasksRetried.WithLabelValues(queue).Inc()
}

// RecordTaskEnqueued records an enqueued task.
func (m *Metrics) RecordTaskEnqueued(queue string) {
	m.TasksEnqueued.WithLabelValues(queue).Inc()
}

// RecordTaskDuration records task processing duration.
func (m *Metrics) RecordTaskDuration(queue, taskType string, duration time.Duration) {
	m.TaskDuration.WithLabelValues(queue, taskType).Observe(duration.Seconds())
}

// SetWorkersActive sets the number of active workers.
func (m *Metrics) SetWorkersActive(count int64) {
	m.WorkersActive.Set(float64(count))
}

// SetWorkersTotal sets the total number of workers.
func (m *Metrics) SetWorkersTotal(count int) {
	m.WorkersTotal.Set(float64(count))
}

// UpdateQueueMetrics updates queue depth and DLQ metrics from the broker.
func (m *Metrics) UpdateQueueMetrics(ctx context.Context) error {
	// Check broker connection
	if err := m.broker.Ping(ctx); err != nil {
		m.BrokerConnected.Set(0)
		return err
	}
	m.BrokerConnected.Set(1)

	// Get queue stats
	stats, err := m.broker.GetQueueStats(ctx)
	if err != nil {
		return err
	}

	for queue, s := range stats {
		m.QueueDepth.WithLabelValues(queue).Set(float64(s.Pending))
		m.QueueActive.WithLabelValues(queue).Set(float64(s.Active))
		m.DLQDepth.WithLabelValues(queue).Set(float64(s.Dead))
	}

	// Get scheduled count
	if rb, ok := m.broker.(*broker.RedisBroker); ok {
		count, err := rb.GetScheduledCount(ctx)
		if err == nil {
			m.ScheduledCount.Set(float64(count))
		}
	}

	return nil
}

// StartCollector starts a background goroutine that periodically updates metrics.
func (m *Metrics) StartCollector(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.UpdateQueueMetrics(ctx)
			}
		}
	}()
}

// Collector implements prometheus.Collector for custom collection.
type Collector struct {
	metrics *Metrics
}

// NewCollector creates a new Collector.
func NewCollector(m *Metrics) *Collector {
	return &Collector{metrics: m}
}

// Describe implements prometheus.Collector.
func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	// Describe is called to get metric descriptions
	c.metrics.QueueDepth.Describe(ch)
	c.metrics.QueueActive.Describe(ch)
	c.metrics.DLQDepth.Describe(ch)
}

// Collect implements prometheus.Collector.
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c.metrics.UpdateQueueMetrics(ctx)

	c.metrics.QueueDepth.Collect(ch)
	c.metrics.QueueActive.Collect(ch)
	c.metrics.DLQDepth.Collect(ch)
}

// Global metrics instance for convenience
var (
	globalMetrics     *Metrics
	globalMetricsOnce sync.Once
)

// Global returns the global metrics instance.
// Must be initialized with InitGlobal first.
func Global() *Metrics {
	return globalMetrics
}

// InitGlobal initializes the global metrics instance.
func InitGlobal(b broker.Broker) *Metrics {
	globalMetricsOnce.Do(func() {
		globalMetrics = NewMetrics(b)
	})
	return globalMetrics
}

// InstrumentedServer wraps a Server to record metrics.
type InstrumentedServer struct {
	*goqueue.Server
	metrics *Metrics
}

// NewInstrumentedServer creates a new instrumented server wrapper.
func NewInstrumentedServer(s *goqueue.Server, m *Metrics) *InstrumentedServer {
	return &InstrumentedServer{
		Server:  s,
		metrics: m,
	}
}

// GetMetrics returns the metrics instance.
func (s *InstrumentedServer) GetMetrics() *Metrics {
	return s.metrics
}
