package goqueue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rustamscode/goqueue/internal/log"
	"github.com/rustamscode/goqueue/pkg/broker"
	"github.com/rustamscode/goqueue/pkg/ratelimit"
)

// Server is a task queue worker that processes tasks from queues.
type Server struct {
	broker       broker.Broker
	config       ServerConfig
	mux          *Mux
	logger       *slog.Logger
	rateLimiters map[string]*ratelimit.TokenBucket

	// Lifecycle management
	started    atomic.Bool
	shutdown   atomic.Bool
	wg         sync.WaitGroup
	cancelFunc context.CancelFunc

	// Metrics
	activeWorkers atomic.Int64
	processed     atomic.Int64
	failed        atomic.Int64
}

// NewServer creates a new Server with the given options.
func NewServer(opts ...ServerOption) (*Server, error) {
	cfg := DefaultServerConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	var redisClient redis.UniversalClient
	if cfg.RedisClient != nil {
		redisClient = cfg.RedisClient
	} else {
		redisClient = redis.NewClient(&redis.Options{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		})
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	// Create broker with retry config
	b := broker.NewRedisBroker(redisClient, broker.WithRetryConfig(broker.RetryConfig{
		MaxRetries: cfg.RetryConfig.MaxRetries,
		BaseDelay:  cfg.RetryConfig.BaseDelay,
		MaxDelay:   cfg.RetryConfig.MaxDelay,
	}))

	// Create logger
	logger := log.NewLogger(log.Config{
		Level:  cfg.LogLevel,
		Format: "json",
	})

	// Create rate limiters
	rateLimiters := make(map[string]*ratelimit.TokenBucket)
	for queue, limit := range cfg.RateLimits {
		rate := float64(limit.Rate) / limit.Interval.Seconds()
		rateLimiters[queue] = ratelimit.NewTokenBucket(rate, float64(limit.Rate))
	}

	return &Server{
		broker:       b,
		config:       cfg,
		mux:          NewMux(),
		logger:       logger,
		rateLimiters: rateLimiters,
	}, nil
}

// Handle registers a handler for tasks of the given type.
func (s *Server) Handle(taskType string, handler Handler) {
	s.mux.Handle(taskType, handler)
}

// HandleFunc registers a handler function for tasks of the given type.
func (s *Server) HandleFunc(taskType string, handler func(ctx context.Context, task *Task) error) {
	s.mux.HandleFunc(taskType, handler)
}

// Start begins processing tasks. It blocks until Shutdown is called.
func (s *Server) Start() error {
	if s.started.Swap(true) {
		return errors.New("server already started")
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cancelFunc = cancel

	// Add logger to context
	ctx = log.WithLogger(ctx, s.logger)

	s.logger.Info("starting goqueue server",
		"concurrency", s.config.Concurrency,
		"queues", s.config.Queues,
	)

	// Build weighted queue list
	queues := s.buildWeightedQueueList()
	if len(queues) == 0 {
		return errors.New("no queues configured")
	}

	// Start worker goroutines
	for i := 0; i < s.config.Concurrency; i++ {
		s.wg.Add(1)
		go s.worker(ctx, i, queues)
	}

	// Wait for shutdown signal
	s.awaitShutdownSignal()

	return nil
}

// Run starts the server and blocks until shutdown.
// It also sets up signal handlers for graceful shutdown.
func (s *Server) Run() error {
	// Start in a goroutine so we can handle signals
	errChan := make(chan error, 1)
	go func() {
		errChan <- s.Start()
	}()

	// Block until Start returns (which happens after shutdown)
	return <-errChan
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	if !s.started.Load() {
		return errors.New("server not started")
	}

	if s.shutdown.Swap(true) {
		return errors.New("shutdown already in progress")
	}

	s.logger.Info("initiating graceful shutdown")

	// Cancel the context to stop accepting new tasks
	if s.cancelFunc != nil {
		s.cancelFunc()
	}

	// Wait for workers to finish with timeout
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Info("graceful shutdown complete",
			"processed", s.processed.Load(),
			"failed", s.failed.Load(),
		)
		return nil
	case <-ctx.Done():
		s.logger.Warn("shutdown timeout exceeded, some tasks may not have completed")
		return ctx.Err()
	}
}

// buildWeightedQueueList creates a queue list weighted by priority.
func (s *Server) buildWeightedQueueList() []string {
	if s.config.StrictPriority {
		// Return queues in priority order (highest weight first)
		queues := make([]string, 0, len(s.config.Queues))
		for queue := range s.config.Queues {
			queues = append(queues, queue)
		}
		return queues
	}

	// Build weighted list where higher weight = more entries
	var queues []string
	for queue, weight := range s.config.Queues {
		for i := 0; i < weight; i++ {
			queues = append(queues, queue)
		}
	}
	return queues
}

// worker is a goroutine that processes tasks from queues.
func (s *Server) worker(ctx context.Context, id int, queues []string) {
	defer s.wg.Done()

	logger := s.logger.With("worker_id", id)
	logger.Debug("worker started")

	for {
		select {
		case <-ctx.Done():
			logger.Debug("worker stopping")
			return
		default:
		}

		// Check rate limits
		if !s.checkRateLimits(queues) {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// Dequeue a task
		task, err := s.broker.Dequeue(ctx, queues, s.config.PollInterval)
		if err != nil {
			if ctx.Err() != nil {
				return // Shutting down
			}
			logger.Error("failed to dequeue task", "error", err)
			time.Sleep(s.config.PollInterval)
			continue
		}

		if task == nil {
			continue // No task available
		}

		// Process the task
		s.activeWorkers.Add(1)
		s.processTask(ctx, task, logger)
		s.activeWorkers.Add(-1)
	}
}

// checkRateLimits checks if processing is allowed for any of the queues.
func (s *Server) checkRateLimits(queues []string) bool {
	if len(s.rateLimiters) == 0 {
		return true
	}

	for _, queue := range queues {
		if limiter, ok := s.rateLimiters[queue]; ok {
			if !limiter.Allow() {
				continue
			}
		}
		return true
	}

	return false
}

// processTask processes a single task.
func (s *Server) processTask(ctx context.Context, task *Task, logger *slog.Logger) {
	logger = logger.With(
		"task_id", task.ID,
		"task_type", task.Type,
		"queue", task.Queue,
	)

	logger.Debug("processing task")
	startTime := time.Now()

	// Create task context with deadline if set
	taskCtx := ctx
	var cancel context.CancelFunc
	if !task.Deadline.IsZero() {
		taskCtx, cancel = context.WithDeadline(ctx, task.Deadline)
	} else {
		taskCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	// Check if task is past deadline
	if task.IsPastDeadline() {
		logger.Warn("task past deadline, marking as failed")
		if err := s.broker.Fail(taskCtx, task, errors.New("task deadline exceeded")); err != nil {
			logger.Error("failed to mark task as failed", "error", err)
		}
		s.failed.Add(1)
		return
	}

	// Execute handler with panic recovery
	err := s.executeHandler(taskCtx, task)

	duration := time.Since(startTime)

	if err != nil {
		logger.Error("task failed",
			"error", err,
			"duration", duration,
			"retry_count", task.RetryCount,
		)

		if failErr := s.broker.Fail(taskCtx, task, err); failErr != nil {
			logger.Error("failed to record task failure", "error", failErr)
		}
		s.failed.Add(1)
		return
	}

	// Mark task as complete
	if err := s.broker.Complete(taskCtx, task); err != nil {
		logger.Error("failed to mark task as complete", "error", err)
		return
	}

	logger.Debug("task completed", "duration", duration)
	s.processed.Add(1)
}

// executeHandler runs the handler with panic recovery.
func (s *Server) executeHandler(ctx context.Context, task *Task) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()

	return s.mux.ProcessTask(ctx, task)
}

// awaitShutdownSignal waits for SIGTERM or SIGINT and initiates shutdown.
func (s *Server) awaitShutdownSignal() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	sig := <-sigChan
	s.logger.Info("received shutdown signal", "signal", sig)

	ctx, cancel := context.WithTimeout(context.Background(), s.config.ShutdownTimeout)
	defer cancel()

	if err := s.Shutdown(ctx); err != nil {
		s.logger.Error("shutdown error", "error", err)
	}
}

// Stats returns current server statistics.
func (s *Server) Stats() ServerStats {
	return ServerStats{
		ActiveWorkers: s.activeWorkers.Load(),
		Processed:     s.processed.Load(),
		Failed:        s.failed.Load(),
	}
}

// ServerStats holds server statistics.
type ServerStats struct {
	ActiveWorkers int64 `json:"active_workers"`
	Processed     int64 `json:"processed"`
	Failed        int64 `json:"failed"`
}

// Broker returns the underlying broker.
func (s *Server) Broker() broker.Broker {
	return s.broker
}

// Logger returns the server's logger.
func (s *Server) Logger() *slog.Logger {
	return s.logger
}

// IsRunning returns true if the server is running.
func (s *Server) IsRunning() bool {
	return s.started.Load() && !s.shutdown.Load()
}
