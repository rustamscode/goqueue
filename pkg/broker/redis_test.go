package broker

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/rustamscode/goqueue/pkg/queue"
)

// setupRedis creates a Redis container for testing.
func setupRedis(t *testing.T) (string, func()) {
	t.Helper()

	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "redis:7-alpine",
		ExposedPorts: []string{"6379/tcp"},
		WaitingFor:   wait.ForLog("Ready to accept connections"),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Skipf("Failed to start Redis container: %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		container.Terminate(ctx)
		t.Fatalf("Failed to get container host: %v", err)
	}

	port, err := container.MappedPort(ctx, "6379")
	if err != nil {
		container.Terminate(ctx)
		t.Fatalf("Failed to get container port: %v", err)
	}

	addr := fmt.Sprintf("%s:%s", host, port.Port())

	cleanup := func() {
		container.Terminate(ctx)
	}

	return addr, cleanup
}

func TestRedisBroker_EnqueueDequeue(t *testing.T) {
	addr, cleanup := setupRedis(t)
	defer cleanup()

	broker, err := NewRedisBrokerFromURL("redis://" + addr)
	if err != nil {
		t.Fatalf("Failed to create broker: %v", err)
	}
	defer broker.Close()

	ctx := context.Background()

	// Create and enqueue a task
	task, _ := queue.NewTask("test:task", map[string]string{"key": "value"})

	err = broker.Enqueue(ctx, task)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	// Dequeue the task
	dequeued, err := broker.Dequeue(ctx, []string{"default"}, 1*time.Second)
	if err != nil {
		t.Fatalf("Dequeue() error = %v", err)
	}

	if dequeued == nil {
		t.Fatal("Dequeue() returned nil task")
	}

	if dequeued.ID != task.ID {
		t.Errorf("Dequeue() task.ID = %s, want %s", dequeued.ID, task.ID)
	}

	if dequeued.State != queue.TaskStateActive {
		t.Errorf("Dequeue() task.State = %s, want %s", dequeued.State, queue.TaskStateActive)
	}
}

func TestRedisBroker_Complete(t *testing.T) {
	addr, cleanup := setupRedis(t)
	defer cleanup()

	broker, err := NewRedisBrokerFromURL("redis://" + addr)
	if err != nil {
		t.Fatalf("Failed to create broker: %v", err)
	}
	defer broker.Close()

	ctx := context.Background()

	// Create, enqueue, and dequeue a task
	task, _ := queue.NewTask("test:task", nil)
	broker.Enqueue(ctx, task)

	dequeued, _ := broker.Dequeue(ctx, []string{"default"}, 1*time.Second)

	// Complete the task
	err = broker.Complete(ctx, dequeued)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	// Verify task state
	stored, err := broker.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}

	if stored.State != queue.TaskStateCompleted {
		t.Errorf("Task state = %s, want %s", stored.State, queue.TaskStateCompleted)
	}
}

func TestRedisBroker_Fail_WithRetry(t *testing.T) {
	addr, cleanup := setupRedis(t)
	defer cleanup()

	broker, err := NewRedisBrokerFromURL("redis://"+addr, WithRetryConfig(RetryConfig{
		MaxRetries: 3,
		BaseDelay:  100 * time.Millisecond,
		MaxDelay:   1 * time.Second,
	}))
	if err != nil {
		t.Fatalf("Failed to create broker: %v", err)
	}
	defer broker.Close()

	ctx := context.Background()

	// Create task with retries enabled
	task, _ := queue.NewTask("test:task", nil)
	task.MaxRetries = 3
	broker.Enqueue(ctx, task)

	dequeued, _ := broker.Dequeue(ctx, []string{"default"}, 1*time.Second)

	// Fail the task
	testErr := errors.New("test error")
	err = broker.Fail(ctx, dequeued, testErr)
	if err != nil {
		t.Fatalf("Fail() error = %v", err)
	}

	// Task should be scheduled for retry
	stored, _ := broker.GetTask(ctx, task.ID)
	if stored.State != queue.TaskStateRetry {
		t.Errorf("Task state = %s, want %s", stored.State, queue.TaskStateRetry)
	}
	if stored.RetryCount != 1 {
		t.Errorf("RetryCount = %d, want 1", stored.RetryCount)
	}
	if stored.LastError != testErr.Error() {
		t.Errorf("LastError = %s, want %s", stored.LastError, testErr.Error())
	}
}

func TestRedisBroker_Fail_MovesToDLQ(t *testing.T) {
	addr, cleanup := setupRedis(t)
	defer cleanup()

	broker, err := NewRedisBrokerFromURL("redis://" + addr)
	if err != nil {
		t.Fatalf("Failed to create broker: %v", err)
	}
	defer broker.Close()

	ctx := context.Background()

	// Create task with no retries left
	task, _ := queue.NewTask("test:task", nil)
	task.MaxRetries = 0 // No retries allowed
	broker.Enqueue(ctx, task)

	dequeued, _ := broker.Dequeue(ctx, []string{"default"}, 1*time.Second)

	// Fail the task
	err = broker.Fail(ctx, dequeued, errors.New("test error"))
	if err != nil {
		t.Fatalf("Fail() error = %v", err)
	}

	// Task should be in DLQ
	stored, _ := broker.GetTask(ctx, task.ID)
	if stored.State != queue.TaskStateDead {
		t.Errorf("Task state = %s, want %s", stored.State, queue.TaskStateDead)
	}

	// Check DLQ
	dlqTasks, err := broker.GetDLQTasks(ctx, "default", 0, 10)
	if err != nil {
		t.Fatalf("GetDLQTasks() error = %v", err)
	}

	found := false
	for _, dlqTask := range dlqTasks {
		if dlqTask.ID == task.ID {
			found = true
			break
		}
	}
	if !found {
		t.Error("Task not found in DLQ")
	}
}

func TestRedisBroker_Schedule(t *testing.T) {
	addr, cleanup := setupRedis(t)
	defer cleanup()

	broker, err := NewRedisBrokerFromURL("redis://" + addr)
	if err != nil {
		t.Fatalf("Failed to create broker: %v", err)
	}
	defer broker.Close()

	ctx := context.Background()

	// Schedule task for future
	task, _ := queue.NewTask("test:task", nil)
	processAt := time.Now().Add(100 * time.Millisecond)

	err = broker.Schedule(ctx, task, processAt)
	if err != nil {
		t.Fatalf("Schedule() error = %v", err)
	}

	// Should not be available immediately
	dequeued, _ := broker.Dequeue(ctx, []string{"default"}, 50*time.Millisecond)
	if dequeued != nil {
		t.Error("Task should not be available before processAt")
	}

	// Wait and promote
	time.Sleep(100 * time.Millisecond)
	promoted, err := broker.PromoteScheduledTasks(ctx, time.Now(), 10)
	if err != nil {
		t.Fatalf("PromoteScheduledTasks() error = %v", err)
	}
	if promoted != 1 {
		t.Errorf("PromoteScheduledTasks() = %d, want 1", promoted)
	}

	// Should be available now
	dequeued, _ = broker.Dequeue(ctx, []string{"default"}, 1*time.Second)
	if dequeued == nil {
		t.Error("Task should be available after promotion")
	}
}

func TestRedisBroker_DLQOperations(t *testing.T) {
	addr, cleanup := setupRedis(t)
	defer cleanup()

	broker, err := NewRedisBrokerFromURL("redis://" + addr)
	if err != nil {
		t.Fatalf("Failed to create broker: %v", err)
	}
	defer broker.Close()

	ctx := context.Background()

	// Create and move task to DLQ
	task, _ := queue.NewTask("test:task", nil)
	task.MaxRetries = 0
	broker.Enqueue(ctx, task)

	dequeued, _ := broker.Dequeue(ctx, []string{"default"}, 1*time.Second)
	broker.Fail(ctx, dequeued, errors.New("error"))

	// Check DLQ size
	size, err := broker.GetDLQSize(ctx, "default")
	if err != nil {
		t.Fatalf("GetDLQSize() error = %v", err)
	}
	if size != 1 {
		t.Errorf("GetDLQSize() = %d, want 1", size)
	}

	// Retry from DLQ
	err = broker.RetryFromDLQ(ctx, "default", task.ID)
	if err != nil {
		t.Fatalf("RetryFromDLQ() error = %v", err)
	}

	// Task should be back in queue
	dequeued, _ = broker.Dequeue(ctx, []string{"default"}, 1*time.Second)
	if dequeued == nil {
		t.Error("Task should be available after retry from DLQ")
	}
	if dequeued.ID != task.ID {
		t.Errorf("Task ID = %s, want %s", dequeued.ID, task.ID)
	}
	if dequeued.RetryCount != 0 {
		t.Errorf("RetryCount = %d, want 0 (reset)", dequeued.RetryCount)
	}
}

func TestRedisBroker_GetQueueStats(t *testing.T) {
	addr, cleanup := setupRedis(t)
	defer cleanup()

	broker, err := NewRedisBrokerFromURL("redis://" + addr)
	if err != nil {
		t.Fatalf("Failed to create broker: %v", err)
	}
	defer broker.Close()

	ctx := context.Background()

	// Enqueue some tasks
	for i := 0; i < 5; i++ {
		task, _ := queue.NewTask("test:task", nil)
		broker.Enqueue(ctx, task)
	}

	// Get stats
	stats, err := broker.GetQueueStats(ctx)
	if err != nil {
		t.Fatalf("GetQueueStats() error = %v", err)
	}

	defaultStats, ok := stats["default"]
	if !ok {
		t.Fatal("default queue not found in stats")
	}

	if defaultStats.Pending != 5 {
		t.Errorf("Pending = %d, want 5", defaultStats.Pending)
	}
}

func TestRedisBroker_Priority(t *testing.T) {
	addr, cleanup := setupRedis(t)
	defer cleanup()

	broker, err := NewRedisBrokerFromURL("redis://" + addr)
	if err != nil {
		t.Fatalf("Failed to create broker: %v", err)
	}
	defer broker.Close()

	ctx := context.Background()

	// Enqueue tasks with different priorities
	lowTask, _ := queue.NewTask("test:low", nil)
	lowTask.Priority = queue.PriorityLow
	lowTask.CreatedAt = time.Now()

	highTask, _ := queue.NewTask("test:high", nil)
	highTask.Priority = queue.PriorityHigh
	highTask.CreatedAt = time.Now().Add(1 * time.Millisecond) // Created after low

	// Enqueue low first, then high
	broker.Enqueue(ctx, lowTask)
	broker.Enqueue(ctx, highTask)

	// High priority should be dequeued first despite being enqueued second
	first, _ := broker.Dequeue(ctx, []string{"default"}, 1*time.Second)
	if first.ID != highTask.ID {
		t.Errorf("First dequeued = %s, want %s (high priority)", first.Type, highTask.Type)
	}

	second, _ := broker.Dequeue(ctx, []string{"default"}, 1*time.Second)
	if second.ID != lowTask.ID {
		t.Errorf("Second dequeued = %s, want %s (low priority)", second.Type, lowTask.Type)
	}
}

func TestRedisBroker_Ping(t *testing.T) {
	addr, cleanup := setupRedis(t)
	defer cleanup()

	broker, err := NewRedisBrokerFromURL("redis://" + addr)
	if err != nil {
		t.Fatalf("Failed to create broker: %v", err)
	}
	defer broker.Close()

	ctx := context.Background()
	if err := broker.Ping(ctx); err != nil {
		t.Errorf("Ping() error = %v", err)
	}
}

func BenchmarkRedisBroker_Enqueue(b *testing.B) {
	addr, cleanup := setupRedis(&testing.T{})
	defer cleanup()

	broker, err := NewRedisBrokerFromURL("redis://" + addr)
	if err != nil {
		b.Skipf("Failed to create broker: %v", err)
	}
	defer broker.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		task, _ := queue.NewTask("test:task", nil)
		broker.Enqueue(ctx, task)
	}
}

func BenchmarkRedisBroker_EnqueueDequeue(b *testing.B) {
	addr, cleanup := setupRedis(&testing.T{})
	defer cleanup()

	broker, err := NewRedisBrokerFromURL("redis://" + addr)
	if err != nil {
		b.Skipf("Failed to create broker: %v", err)
	}
	defer broker.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		task, _ := queue.NewTask("test:task", nil)
		broker.Enqueue(ctx, task)
		broker.Dequeue(ctx, []string{"default"}, 1*time.Second)
	}
}
