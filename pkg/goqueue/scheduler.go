package goqueue

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rustamscode/goqueue/internal/log"
	"github.com/rustamscode/goqueue/internal/timeutil"
	"github.com/rustamscode/goqueue/pkg/broker"
)

// Scheduler manages delayed and periodic tasks.
// It polls for scheduled tasks and moves them to their queues when ready.
type Scheduler struct {
	broker broker.Broker
	logger *slog.Logger
	config SchedulerConfig

	periodicTasks map[string]*PeriodicTask
	periodicMu    sync.RWMutex

	running atomic.Bool
	wg      sync.WaitGroup
}

// SchedulerConfig holds configuration for the Scheduler.
type SchedulerConfig struct {
	// PollInterval is how often to check for scheduled tasks (default: 1s).
	PollInterval time.Duration
	// BatchSize is the maximum number of tasks to promote per poll (default: 100).
	BatchSize int
	// LogLevel is the logging level.
	LogLevel string
}

// DefaultSchedulerConfig returns the default scheduler configuration.
func DefaultSchedulerConfig() SchedulerConfig {
	return SchedulerConfig{
		PollInterval: 1 * time.Second,
		BatchSize:    100,
		LogLevel:     "info",
	}
}

// SchedulerOption configures the Scheduler.
type SchedulerOption func(*SchedulerConfig)

// WithSchedulerPollInterval sets the polling interval.
func WithSchedulerPollInterval(interval time.Duration) SchedulerOption {
	return func(c *SchedulerConfig) {
		c.PollInterval = interval
	}
}

// WithSchedulerBatchSize sets the batch size for promoting tasks.
func WithSchedulerBatchSize(size int) SchedulerOption {
	return func(c *SchedulerConfig) {
		c.BatchSize = size
	}
}

// WithSchedulerLogLevel sets the logging level.
func WithSchedulerLogLevel(level string) SchedulerOption {
	return func(c *SchedulerConfig) {
		c.LogLevel = level
	}
}

// PeriodicTask represents a recurring task.
type PeriodicTask struct {
	// ID is the unique identifier for this periodic task.
	ID string
	// Spec is the interval specification (e.g., "@every 5m", "@daily").
	Spec string
	// Task is the task template to enqueue.
	Task *Task
	// Interval is the parsed duration between executions.
	Interval time.Duration
	// LastRun is when the task was last enqueued.
	LastRun time.Time
	// NextRun is when the task will next be enqueued.
	NextRun time.Time
}

// NewScheduler creates a new Scheduler with the given broker and options.
func NewScheduler(b broker.Broker, opts ...SchedulerOption) *Scheduler {
	cfg := DefaultSchedulerConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	logger := log.NewLogger(log.Config{
		Level:  cfg.LogLevel,
		Format: "json",
	})

	return &Scheduler{
		broker:        b,
		logger:        logger,
		config:        cfg,
		periodicTasks: make(map[string]*PeriodicTask),
	}
}

// RegisterPeriodic registers a periodic task with the given interval spec.
// Supported specs:
//   - @every <duration>: e.g., "@every 5m", "@every 1h30m"
//   - @daily: once per day at midnight UTC
//   - @hourly: once per hour at minute 0
func (s *Scheduler) RegisterPeriodic(id, spec string, task *Task) error {
	interval, ok := timeutil.ParseInterval(spec)
	if !ok {
		return &InvalidIntervalError{Spec: spec}
	}

	s.periodicMu.Lock()
	defer s.periodicMu.Unlock()

	s.periodicTasks[id] = &PeriodicTask{
		ID:       id,
		Spec:     spec,
		Task:     task,
		Interval: interval,
		NextRun:  time.Now().Add(interval),
	}

	s.logger.Info("registered periodic task",
		"id", id,
		"spec", spec,
		"interval", interval,
	)

	return nil
}

// UnregisterPeriodic removes a periodic task.
func (s *Scheduler) UnregisterPeriodic(id string) {
	s.periodicMu.Lock()
	defer s.periodicMu.Unlock()

	delete(s.periodicTasks, id)
	s.logger.Info("unregistered periodic task", "id", id)
}

// Start begins the scheduler loop.
func (s *Scheduler) Start(ctx context.Context) error {
	if s.running.Swap(true) {
		return &SchedulerError{Message: "scheduler already running"}
	}

	s.logger.Info("starting scheduler",
		"poll_interval", s.config.PollInterval,
		"batch_size", s.config.BatchSize,
	)

	s.wg.Add(1)
	go s.loop(ctx)

	return nil
}

// Stop gracefully stops the scheduler.
func (s *Scheduler) Stop() {
	if !s.running.Load() {
		return
	}

	s.running.Store(false)
	s.wg.Wait()
	s.logger.Info("scheduler stopped")
}

// loop is the main scheduler loop.
func (s *Scheduler) loop(ctx context.Context) {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.running.Store(false)
			return
		case <-ticker.C:
			if !s.running.Load() {
				return
			}
			s.tick(ctx)
		}
	}
}

// tick performs one scheduler iteration.
func (s *Scheduler) tick(ctx context.Context) {
	// Promote scheduled tasks
	promoted, err := s.broker.PromoteScheduledTasks(ctx, time.Now(), s.config.BatchSize)
	if err != nil {
		s.logger.Error("failed to promote scheduled tasks", "error", err)
	} else if promoted > 0 {
		s.logger.Debug("promoted scheduled tasks", "count", promoted)
	}

	// Process periodic tasks
	s.processPeriodicTasks(ctx)
}

// processPeriodicTasks enqueues periodic tasks that are due.
func (s *Scheduler) processPeriodicTasks(ctx context.Context) {
	s.periodicMu.RLock()
	tasks := make([]*PeriodicTask, 0, len(s.periodicTasks))
	for _, pt := range s.periodicTasks {
		tasks = append(tasks, pt)
	}
	s.periodicMu.RUnlock()

	now := time.Now()
	for _, pt := range tasks {
		if now.Before(pt.NextRun) {
			continue
		}

		// Clone the task template with a new ID
		task := pt.Task.Clone()
		task.ID = newTaskID()
		task.CreatedAt = now

		// Enqueue the task
		if err := s.broker.Enqueue(ctx, task); err != nil {
			s.logger.Error("failed to enqueue periodic task",
				"id", pt.ID,
				"error", err,
			)
			continue
		}

		s.logger.Debug("enqueued periodic task",
			"id", pt.ID,
			"task_id", task.ID,
		)

		// Update timing
		s.periodicMu.Lock()
		if periodic, ok := s.periodicTasks[pt.ID]; ok {
			periodic.LastRun = now
			periodic.NextRun = now.Add(periodic.Interval)
		}
		s.periodicMu.Unlock()
	}
}

// ListPeriodic returns all registered periodic tasks.
func (s *Scheduler) ListPeriodic() []*PeriodicTask {
	s.periodicMu.RLock()
	defer s.periodicMu.RUnlock()

	tasks := make([]*PeriodicTask, 0, len(s.periodicTasks))
	for _, pt := range s.periodicTasks {
		tasks = append(tasks, pt)
	}
	return tasks
}

// newTaskID generates a new task ID.
func newTaskID() string {
	return NewTaskWithPayload("", nil).ID
}

// InvalidIntervalError is returned when an interval spec cannot be parsed.
type InvalidIntervalError struct {
	Spec string
}

func (e *InvalidIntervalError) Error() string {
	return "invalid interval spec: " + e.Spec
}

// SchedulerError represents a scheduler error.
type SchedulerError struct {
	Message string
}

func (e *SchedulerError) Error() string {
	return "scheduler: " + e.Message
}
