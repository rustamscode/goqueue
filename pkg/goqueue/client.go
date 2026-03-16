package goqueue

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rustamscode/goqueue/pkg/broker"
)

// Client is a task queue producer that enqueues tasks for processing.
type Client struct {
	broker broker.Broker
	config ClientConfig
}

// NewClient creates a new Client with the given options.
func NewClient(opts ...ClientOption) (*Client, error) {
	cfg := DefaultClientConfig()
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

	b := broker.NewRedisBroker(redisClient)

	return &Client{
		broker: b,
		config: cfg,
	}, nil
}

// Enqueue adds a task to the queue for processing.
func (c *Client) Enqueue(ctx context.Context, task *Task, opts ...EnqueueOption) (*TaskInfo, error) {
	cfg := &EnqueueConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// Apply options to task
	if cfg.Queue != "" {
		task.Queue = cfg.Queue
	}
	if cfg.Priority != 0 {
		task.Priority = cfg.Priority
	}
	if cfg.MaxRetries > 0 {
		task.MaxRetries = cfg.MaxRetries
	}
	if !cfg.Deadline.IsZero() {
		task.Deadline = cfg.Deadline
	}
	if cfg.Timeout > 0 {
		task.Deadline = time.Now().Add(cfg.Timeout)
	}

	// Handle delayed scheduling
	processAt := cfg.ProcessAt
	if cfg.ProcessIn > 0 {
		processAt = time.Now().Add(cfg.ProcessIn)
	}

	var err error
	if !processAt.IsZero() && processAt.After(time.Now()) {
		// Schedule for future processing
		err = c.broker.Schedule(ctx, task, processAt)
	} else {
		// Immediate enqueue
		err = c.broker.Enqueue(ctx, task)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to enqueue task: %w", err)
	}

	return &TaskInfo{
		ID:        task.ID,
		Queue:     task.Queue,
		State:     task.State,
		ProcessAt: processAt,
	}, nil
}

// EnqueueAt schedules a task to be processed at the specified time.
func (c *Client) EnqueueAt(ctx context.Context, task *Task, processAt time.Time, opts ...EnqueueOption) (*TaskInfo, error) {
	opts = append(opts, ProcessAt(processAt))
	return c.Enqueue(ctx, task, opts...)
}

// EnqueueIn schedules a task to be processed after the specified delay.
func (c *Client) EnqueueIn(ctx context.Context, task *Task, delay time.Duration, opts ...EnqueueOption) (*TaskInfo, error) {
	opts = append(opts, ProcessIn(delay))
	return c.Enqueue(ctx, task, opts...)
}

// GetTask retrieves a task by ID.
func (c *Client) GetTask(ctx context.Context, taskID string) (*Task, error) {
	return c.broker.GetTask(ctx, taskID)
}

// GetQueueStats returns statistics for all queues.
func (c *Client) GetQueueStats(ctx context.Context) (map[string]*QueueStats, error) {
	return c.broker.GetQueueStats(ctx)
}

// GetQueues returns a list of all known queue names.
func (c *Client) GetQueues(ctx context.Context) ([]string, error) {
	return c.broker.GetQueues(ctx)
}

// Ping checks the connection to the broker.
func (c *Client) Ping(ctx context.Context) error {
	return c.broker.Ping(ctx)
}

// Close closes the client connection.
func (c *Client) Close() error {
	return c.broker.Close()
}

// Broker returns the underlying broker for advanced operations.
func (c *Client) Broker() broker.Broker {
	return c.broker
}
