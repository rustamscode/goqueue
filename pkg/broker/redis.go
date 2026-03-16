package broker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rustamscode/goqueue/internal/timeutil"
	"github.com/rustamscode/goqueue/pkg/queue"
)

const (
	// Redis key prefixes
	keyPrefix          = "goqueue:"
	keyPending         = keyPrefix + "queue:%s:pending"   // Sorted set: task_id -> priority score
	keyActive          = keyPrefix + "queue:%s:active"    // Hash: task_id -> task JSON
	keyScheduled       = keyPrefix + "scheduled"          // Sorted set: task_id -> process_at timestamp
	keyDLQ             = keyPrefix + "dlq:%s"             // List: task JSON (LIFO)
	keyTask            = keyPrefix + "task:%s"            // String: task JSON
	keyQueues          = keyPrefix + "queues"             // Set: queue names
	keyStats           = keyPrefix + "stats:%s"           // Hash: queue statistics
	keyProcessedCount  = keyPrefix + "stats:%s:processed" // Counter for processed tasks
	keyActiveWorkers   = keyPrefix + "workers:active"     // Set: active worker IDs

	// Default visibility timeout for active tasks
	defaultVisibilityTimeout = 30 * time.Minute
)

// RedisBroker implements the Broker interface using Redis.
type RedisBroker struct {
	client            redis.UniversalClient
	retryConfig       RetryConfig
	visibilityTimeout time.Duration
}

// RedisBrokerOption configures the Redis broker.
type RedisBrokerOption func(*RedisBroker)

// WithRetryConfig sets the retry configuration.
func WithRetryConfig(cfg RetryConfig) RedisBrokerOption {
	return func(b *RedisBroker) {
		b.retryConfig = cfg
	}
}

// WithVisibilityTimeout sets the visibility timeout for active tasks.
func WithVisibilityTimeout(timeout time.Duration) RedisBrokerOption {
	return func(b *RedisBroker) {
		b.visibilityTimeout = timeout
	}
}

// NewRedisBroker creates a new Redis broker with the given client.
func NewRedisBroker(client redis.UniversalClient, opts ...RedisBrokerOption) *RedisBroker {
	b := &RedisBroker{
		client:            client,
		retryConfig:       DefaultRetryConfig(),
		visibilityTimeout: defaultVisibilityTimeout,
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// NewRedisBrokerFromURL creates a new Redis broker from a connection URL.
func NewRedisBrokerFromURL(redisURL string, opts ...RedisBrokerOption) (*RedisBroker, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse redis URL: %w", err)
	}
	client := redis.NewClient(opt)
	return NewRedisBroker(client, opts...), nil
}

// pendingKey returns the Redis key for a queue's pending tasks.
func pendingKey(queue string) string {
	return fmt.Sprintf(keyPending, queue)
}

// activeKey returns the Redis key for a queue's active tasks.
func activeKey(queue string) string {
	return fmt.Sprintf(keyActive, queue)
}

// dlqKey returns the Redis key for a queue's dead letter queue.
func dlqKey(queue string) string {
	return fmt.Sprintf(keyDLQ, queue)
}

// taskKey returns the Redis key for storing a task.
func taskKey(taskID string) string {
	return fmt.Sprintf(keyTask, taskID)
}

// statsKey returns the Redis key for queue statistics.
func statsKey(queue string) string {
	return fmt.Sprintf(keyStats, queue)
}

// calculatePriorityScore calculates the score for sorted set ordering.
// Higher priority tasks get lower scores (processed first).
// Within the same priority, earlier tasks get lower scores (FIFO).
// Score = (maxPriority - priority) * 1e12 + timestamp_nano
func calculatePriorityScore(priority queue.Priority, createdAt time.Time) float64 {
	const maxPriority = 10
	priorityWeight := float64(maxPriority - int(priority))
	timestamp := float64(createdAt.UnixNano())
	return priorityWeight*1e12 + timestamp/1e9
}

// Enqueue adds a task to the specified queue.
func (b *RedisBroker) Enqueue(ctx context.Context, task *queue.Task) error {
	if err := task.Validate(); err != nil {
		return fmt.Errorf("invalid task: %w", err)
	}

	task.State = queue.TaskStatePending

	taskJSON, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	score := calculatePriorityScore(task.Priority, task.CreatedAt)

	pipe := b.client.Pipeline()
	pipe.ZAdd(ctx, pendingKey(task.Queue), redis.Z{
		Score:  score,
		Member: task.ID,
	})
	pipe.Set(ctx, taskKey(task.ID), taskJSON, 24*time.Hour)
	pipe.SAdd(ctx, keyQueues, task.Queue)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to enqueue task: %w", err)
	}

	return nil
}

// Dequeue retrieves the next task from the specified queues.
func (b *RedisBroker) Dequeue(ctx context.Context, queues []string, timeout time.Duration) (*queue.Task, error) {
	if len(queues) == 0 {
		return nil, errors.New("no queues specified")
	}

	// Build list of pending queue keys
	keys := make([]string, len(queues))
	for i, q := range queues {
		keys[i] = pendingKey(q)
	}

	// Use BZPOPMIN for blocking pop with timeout
	result, err := b.client.BZPopMin(ctx, timeout, keys...).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil // No task available
		}
		return nil, fmt.Errorf("failed to dequeue task: %w", err)
	}

	taskID := result.Member.(string)

	// Get the task data
	taskJSON, err := b.client.Get(ctx, taskKey(taskID)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			// Task was deleted, try again
			return b.Dequeue(ctx, queues, timeout)
		}
		return nil, fmt.Errorf("failed to get task data: %w", err)
	}

	var task queue.Task
	if err := json.Unmarshal(taskJSON, &task); err != nil {
		return nil, fmt.Errorf("failed to unmarshal task: %w", err)
	}

	// Mark task as active
	task.State = queue.TaskStateActive
	task.StartedAt = time.Now().UTC()

	updatedJSON, err := json.Marshal(&task)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal updated task: %w", err)
	}

	// Move to active set with visibility timeout
	pipe := b.client.Pipeline()
	pipe.HSet(ctx, activeKey(task.Queue), task.ID, updatedJSON)
	pipe.Set(ctx, taskKey(task.ID), updatedJSON, 24*time.Hour)
	pipe.HIncrBy(ctx, statsKey(task.Queue), "dequeued", 1)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to mark task as active: %w", err)
	}

	return &task, nil
}

// Complete marks a task as successfully completed.
func (b *RedisBroker) Complete(ctx context.Context, task *queue.Task) error {
	task.State = queue.TaskStateCompleted
	task.CompletedAt = time.Now().UTC()

	taskJSON, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	pipe := b.client.Pipeline()
	pipe.HDel(ctx, activeKey(task.Queue), task.ID)
	pipe.Set(ctx, taskKey(task.ID), taskJSON, 1*time.Hour) // Keep completed task briefly
	pipe.HIncrBy(ctx, statsKey(task.Queue), "completed", 1)
	pipe.HIncrBy(ctx, statsKey(task.Queue), "processed", 1)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to complete task: %w", err)
	}

	return nil
}

// Fail marks a task as failed and handles retry logic.
func (b *RedisBroker) Fail(ctx context.Context, task *queue.Task, taskErr error) error {
	task.State = queue.TaskStateFailed
	task.RetryCount++
	if taskErr != nil {
		task.LastError = taskErr.Error()
	}

	// Check if we should retry
	if task.CanRetry() {
		return b.scheduleRetry(ctx, task)
	}

	// Max retries exhausted, move to DLQ
	return b.MoveToDLQ(ctx, task)
}

// scheduleRetry schedules a task for retry with exponential backoff.
func (b *RedisBroker) scheduleRetry(ctx context.Context, task *queue.Task) error {
	task.State = queue.TaskStateRetry

	// Calculate retry delay with exponential backoff
	backoffCfg := timeutil.BackoffConfig{
		BaseDelay:    b.retryConfig.BaseDelay,
		MaxDelay:     b.retryConfig.MaxDelay,
		Multiplier:   2.0,
		JitterFactor: 0.5,
	}
	delay := backoffCfg.Calculate(task.RetryCount - 1) // -1 because we already incremented
	processAt := time.Now().Add(delay)

	taskJSON, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	pipe := b.client.Pipeline()
	pipe.HDel(ctx, activeKey(task.Queue), task.ID)
	pipe.ZAdd(ctx, keyScheduled, redis.Z{
		Score:  float64(processAt.Unix()),
		Member: task.ID,
	})
	pipe.Set(ctx, taskKey(task.ID), taskJSON, 24*time.Hour)
	pipe.HIncrBy(ctx, statsKey(task.Queue), "retried", 1)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to schedule retry: %w", err)
	}

	return nil
}

// Schedule adds a task to be processed at a future time.
func (b *RedisBroker) Schedule(ctx context.Context, task *queue.Task, processAt time.Time) error {
	if err := task.Validate(); err != nil {
		return fmt.Errorf("invalid task: %w", err)
	}

	task.ProcessAt = processAt
	task.State = queue.TaskStatePending

	taskJSON, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	pipe := b.client.Pipeline()
	pipe.ZAdd(ctx, keyScheduled, redis.Z{
		Score:  float64(processAt.Unix()),
		Member: task.ID,
	})
	pipe.Set(ctx, taskKey(task.ID), taskJSON, 24*time.Hour)
	pipe.SAdd(ctx, keyQueues, task.Queue)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to schedule task: %w", err)
	}

	return nil
}

// GetScheduledTasks retrieves tasks that are ready to be processed.
func (b *RedisBroker) GetScheduledTasks(ctx context.Context, now time.Time, limit int) ([]*queue.Task, error) {
	// Get task IDs that are due
	taskIDs, err := b.client.ZRangeByScore(ctx, keyScheduled, &redis.ZRangeBy{
		Min:   "-inf",
		Max:   strconv.FormatInt(now.Unix(), 10),
		Count: int64(limit),
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get scheduled tasks: %w", err)
	}

	if len(taskIDs) == 0 {
		return nil, nil
	}

	// Get task data
	tasks := make([]*queue.Task, 0, len(taskIDs))
	for _, id := range taskIDs {
		taskJSON, err := b.client.Get(ctx, taskKey(id)).Bytes()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				continue // Task was deleted
			}
			return nil, fmt.Errorf("failed to get task %s: %w", id, err)
		}

		var task queue.Task
		if err := json.Unmarshal(taskJSON, &task); err != nil {
			continue // Skip malformed tasks
		}
		tasks = append(tasks, &task)
	}

	return tasks, nil
}

// PromoteScheduledTasks moves ready scheduled tasks to their queues.
func (b *RedisBroker) PromoteScheduledTasks(ctx context.Context, now time.Time, limit int) (int, error) {
	tasks, err := b.GetScheduledTasks(ctx, now, limit)
	if err != nil {
		return 0, err
	}

	promoted := 0
	for _, task := range tasks {
		// Remove from scheduled set
		removed, err := b.client.ZRem(ctx, keyScheduled, task.ID).Result()
		if err != nil {
			continue
		}
		if removed == 0 {
			continue // Already promoted by another worker
		}

		// Reset state for retry tasks
		if task.State == queue.TaskStateRetry {
			task.State = queue.TaskStatePending
		}

		// Enqueue to the appropriate queue
		if err := b.Enqueue(ctx, task); err != nil {
			// Re-add to scheduled set if enqueue fails
			b.client.ZAdd(ctx, keyScheduled, redis.Z{
				Score:  float64(task.ProcessAt.Unix()),
				Member: task.ID,
			})
			continue
		}

		promoted++
	}

	return promoted, nil
}

// MoveToDLQ moves a task to the dead letter queue.
func (b *RedisBroker) MoveToDLQ(ctx context.Context, task *queue.Task) error {
	task.State = queue.TaskStateDead

	taskJSON, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	pipe := b.client.Pipeline()
	pipe.HDel(ctx, activeKey(task.Queue), task.ID)
	pipe.LPush(ctx, dlqKey(task.Queue), taskJSON)
	pipe.Set(ctx, taskKey(task.ID), taskJSON, 7*24*time.Hour) // Keep DLQ tasks longer
	pipe.HIncrBy(ctx, statsKey(task.Queue), "dead", 1)
	pipe.HIncrBy(ctx, statsKey(task.Queue), "failed", 1)
	pipe.HIncrBy(ctx, statsKey(task.Queue), "processed", 1)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to move task to DLQ: %w", err)
	}

	return nil
}

// RetryFromDLQ moves a task from DLQ back to its original queue.
func (b *RedisBroker) RetryFromDLQ(ctx context.Context, queueName string, taskID string) error {
	// Get task from storage
	taskJSON, err := b.client.Get(ctx, taskKey(taskID)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return fmt.Errorf("task not found: %s", taskID)
		}
		return fmt.Errorf("failed to get task: %w", err)
	}

	var task queue.Task
	if err := json.Unmarshal(taskJSON, &task); err != nil {
		return fmt.Errorf("failed to unmarshal task: %w", err)
	}

	// Reset retry count and state
	task.RetryCount = 0
	task.State = queue.TaskStatePending
	task.LastError = ""

	// Remove from DLQ and re-enqueue
	if err := b.removeFromDLQ(ctx, queueName, taskID); err != nil {
		return err
	}

	if err := b.Enqueue(ctx, &task); err != nil {
		return fmt.Errorf("failed to re-enqueue task: %w", err)
	}

	return nil
}

// removeFromDLQ removes a task from the DLQ by ID.
func (b *RedisBroker) removeFromDLQ(ctx context.Context, queueName string, taskID string) error {
	// Get all DLQ tasks
	tasks, err := b.client.LRange(ctx, dlqKey(queueName), 0, -1).Result()
	if err != nil {
		return fmt.Errorf("failed to get DLQ tasks: %w", err)
	}

	for _, taskJSON := range tasks {
		var t queue.Task
		if err := json.Unmarshal([]byte(taskJSON), &t); err != nil {
			continue
		}
		if t.ID == taskID {
			b.client.LRem(ctx, dlqKey(queueName), 1, taskJSON)
			return nil
		}
	}

	return nil
}

// GetDLQTasks retrieves tasks from the dead letter queue.
func (b *RedisBroker) GetDLQTasks(ctx context.Context, queueName string, offset, limit int) ([]*queue.Task, error) {
	taskJSONs, err := b.client.LRange(ctx, dlqKey(queueName), int64(offset), int64(offset+limit-1)).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get DLQ tasks: %w", err)
	}

	tasks := make([]*queue.Task, 0, len(taskJSONs))
	for _, taskJSON := range taskJSONs {
		var t queue.Task
		if err := json.Unmarshal([]byte(taskJSON), &t); err != nil {
			continue
		}
		tasks = append(tasks, &t)
	}

	return tasks, nil
}

// GetDLQSize returns the number of tasks in the dead letter queue.
func (b *RedisBroker) GetDLQSize(ctx context.Context, queueName string) (int64, error) {
	return b.client.LLen(ctx, dlqKey(queueName)).Result()
}

// PurgeDLQ removes all tasks from the dead letter queue.
func (b *RedisBroker) PurgeDLQ(ctx context.Context, queueName string) error {
	return b.client.Del(ctx, dlqKey(queueName)).Err()
}

// DeleteFromDLQ removes a specific task from the dead letter queue.
func (b *RedisBroker) DeleteFromDLQ(ctx context.Context, queueName string, taskID string) error {
	return b.removeFromDLQ(ctx, queueName, taskID)
}

// GetTask retrieves a task by ID.
func (b *RedisBroker) GetTask(ctx context.Context, taskID string) (*queue.Task, error) {
	taskJSON, err := b.client.Get(ctx, taskKey(taskID)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get task: %w", err)
	}

	var task queue.Task
	if err := json.Unmarshal(taskJSON, &task); err != nil {
		return nil, fmt.Errorf("failed to unmarshal task: %w", err)
	}

	return &task, nil
}

// GetQueueStats returns statistics for all queues.
func (b *RedisBroker) GetQueueStats(ctx context.Context) (map[string]*queue.QueueStats, error) {
	queueNames, err := b.GetQueues(ctx)
	if err != nil {
		return nil, err
	}

	stats := make(map[string]*queue.QueueStats, len(queueNames))
	for _, qName := range queueNames {
		qStats, err := b.getQueueStats(ctx, qName)
		if err != nil {
			continue
		}
		stats[qName] = qStats
	}

	return stats, nil
}

// getQueueStats returns statistics for a single queue.
func (b *RedisBroker) getQueueStats(ctx context.Context, queueName string) (*queue.QueueStats, error) {
	pipe := b.client.Pipeline()

	pendingCmd := pipe.ZCard(ctx, pendingKey(queueName))
	activeCmd := pipe.HLen(ctx, activeKey(queueName))
	dlqCmd := pipe.LLen(ctx, dlqKey(queueName))
	statsCmd := pipe.HGetAll(ctx, statsKey(queueName))

	_, err := pipe.Exec(ctx)
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("failed to get queue stats: %w", err)
	}

	statsMap := statsCmd.Val()

	completed, _ := strconv.ParseInt(statsMap["completed"], 10, 64)
	failed, _ := strconv.ParseInt(statsMap["failed"], 10, 64)
	processed, _ := strconv.ParseInt(statsMap["processed"], 10, 64)

	return &queue.QueueStats{
		Name:      queueName,
		Pending:   pendingCmd.Val(),
		Active:    activeCmd.Val(),
		Dead:      dlqCmd.Val(),
		Completed: completed,
		Failed:    failed,
		Processed: processed,
	}, nil
}

// GetQueues returns a list of all known queue names.
func (b *RedisBroker) GetQueues(ctx context.Context) ([]string, error) {
	queues, err := b.client.SMembers(ctx, keyQueues).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get queues: %w", err)
	}

	// Always include default queue
	hasDefault := false
	for _, q := range queues {
		if q == "default" {
			hasDefault = true
			break
		}
	}
	if !hasDefault {
		queues = append(queues, "default")
	}

	return queues, nil
}

// Ping checks the connection to the broker.
func (b *RedisBroker) Ping(ctx context.Context) error {
	return b.client.Ping(ctx).Err()
}

// Close closes the connection to the broker.
func (b *RedisBroker) Close() error {
	return b.client.Close()
}

// RequeueStaleActiveTasks moves tasks that have been active too long back to pending.
// This handles tasks from workers that crashed without completing.
func (b *RedisBroker) RequeueStaleActiveTasks(ctx context.Context, queueName string, maxAge time.Duration) (int, error) {
	activeTasks, err := b.client.HGetAll(ctx, activeKey(queueName)).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to get active tasks: %w", err)
	}

	requeued := 0
	cutoff := time.Now().Add(-maxAge)

	for _, taskJSON := range activeTasks {
		var t queue.Task
		if err := json.Unmarshal([]byte(taskJSON), &t); err != nil {
			continue
		}

		if t.StartedAt.Before(cutoff) {
			// Task has been active too long, requeue it
			t.State = queue.TaskStatePending
			t.StartedAt = time.Time{}

			if err := b.client.HDel(ctx, activeKey(queueName), t.ID).Err(); err != nil {
				continue
			}

			if err := b.Enqueue(ctx, &t); err != nil {
				continue
			}

			requeued++
		}
	}

	return requeued, nil
}

// GetScheduledCount returns the number of scheduled tasks.
func (b *RedisBroker) GetScheduledCount(ctx context.Context) (int64, error) {
	return b.client.ZCard(ctx, keyScheduled).Result()
}

// parseQueueFromKey extracts queue name from a Redis key.
func parseQueueFromKey(key string) string {
	// Key format: goqueue:queue:{name}:pending
	parts := strings.Split(key, ":")
	if len(parts) >= 3 {
		return parts[2]
	}
	return ""
}
