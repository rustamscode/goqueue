// Package broker provides the task queue broker interface and implementations.
package broker

import (
	"context"
	"time"

	"github.com/rustamscode/goqueue/pkg/queue"
)

// Broker defines the interface for task queue storage and operations.
type Broker interface {
	// Enqueue adds a task to the specified queue.
	Enqueue(ctx context.Context, task *queue.Task) error

	// Dequeue retrieves the next task from the specified queues.
	// The timeout parameter specifies how long to wait for a task.
	// Returns nil if no task is available within the timeout.
	Dequeue(ctx context.Context, queues []string, timeout time.Duration) (*queue.Task, error)

	// Complete marks a task as successfully completed.
	Complete(ctx context.Context, task *queue.Task) error

	// Fail marks a task as failed and handles retry logic.
	// If the task can be retried, it schedules a retry.
	// If max retries exhausted, moves task to DLQ.
	Fail(ctx context.Context, task *queue.Task, taskErr error) error

	// Schedule adds a task to be processed at a future time.
	Schedule(ctx context.Context, task *queue.Task, processAt time.Time) error

	// GetScheduledTasks retrieves tasks that are ready to be processed.
	GetScheduledTasks(ctx context.Context, now time.Time, limit int) ([]*queue.Task, error)

	// PromoteScheduledTasks moves ready scheduled tasks to their queues.
	PromoteScheduledTasks(ctx context.Context, now time.Time, limit int) (int, error)

	// MoveToDLQ moves a task to the dead letter queue.
	MoveToDLQ(ctx context.Context, task *queue.Task) error

	// RetryFromDLQ moves a task from DLQ back to its original queue.
	RetryFromDLQ(ctx context.Context, queueName string, taskID string) error

	// GetDLQTasks retrieves tasks from the dead letter queue.
	GetDLQTasks(ctx context.Context, queueName string, offset, limit int) ([]*queue.Task, error)

	// GetDLQSize returns the number of tasks in the dead letter queue.
	GetDLQSize(ctx context.Context, queueName string) (int64, error)

	// PurgeDLQ removes all tasks from the dead letter queue.
	PurgeDLQ(ctx context.Context, queueName string) error

	// DeleteFromDLQ removes a specific task from the dead letter queue.
	DeleteFromDLQ(ctx context.Context, queueName string, taskID string) error

	// GetTask retrieves a task by ID.
	GetTask(ctx context.Context, taskID string) (*queue.Task, error)

	// GetQueueStats returns statistics for all queues.
	GetQueueStats(ctx context.Context) (map[string]*queue.QueueStats, error)

	// GetQueues returns a list of all known queue names.
	GetQueues(ctx context.Context) ([]string, error)

	// Ping checks the connection to the broker.
	Ping(ctx context.Context) error

	// Close closes the connection to the broker.
	Close() error
}

// RetryConfig holds configuration for retry behavior.
type RetryConfig struct {
	// MaxRetries is the default maximum number of retries.
	MaxRetries int
	// BaseDelay is the base delay for exponential backoff.
	BaseDelay time.Duration
	// MaxDelay is the maximum delay cap.
	MaxDelay time.Duration
}

// DefaultRetryConfig returns sensible default retry configuration.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries: 3,
		BaseDelay:  1 * time.Second,
		MaxDelay:   1 * time.Hour,
	}
}
