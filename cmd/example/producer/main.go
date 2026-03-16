// Example GoQueue producer that enqueues tasks.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"time"

	"github.com/rustamscode/goqueue/pkg/goqueue"
)

// EmailPayload represents an email task payload.
type EmailPayload struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// ReportPayload represents a report generation task payload.
type ReportPayload struct {
	ReportID   string `json:"report_id"`
	ReportType string `json:"report_type"`
	UserID     string `json:"user_id"`
}

func main() {
	var (
		redisAddr     = flag.String("redis", "localhost:6379", "Redis server address")
		redisPassword = flag.String("redis-password", "", "Redis password")
		count         = flag.Int("count", 100, "Number of tasks to enqueue")
		interval      = flag.Duration("interval", 100*time.Millisecond, "Interval between tasks")
		continuous    = flag.Bool("continuous", false, "Run continuously")
	)
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	// Create client
	client, err := goqueue.NewClient(
		goqueue.WithRedisAddr(*redisAddr),
		goqueue.WithRedisPassword(*redisPassword),
	)
	if err != nil {
		logger.Error("failed to create client", "error", err)
		os.Exit(1)
	}
	defer client.Close()

	fmt.Printf("\n  GoQueue Producer\n")
	fmt.Printf("  Tasks to enqueue: %d\n", *count)
	fmt.Printf("  Interval: %v\n", *interval)
	fmt.Printf("  Continuous: %v\n\n", *continuous)

	ctx := context.Background()
	taskNum := 0

	for {
		for i := 0; i < *count; i++ {
			taskNum++

			// Randomly select task type and queue
			taskType, queue, priority := randomTaskConfig()

			var task *goqueue.Task
			var taskErr error

			switch taskType {
			case "email:send":
				task, taskErr = goqueue.NewTask(taskType, EmailPayload{
					To:      fmt.Sprintf("user%d@example.com", rand.Intn(1000)),
					Subject: fmt.Sprintf("Welcome Email #%d", taskNum),
					Body:    "Thank you for signing up!",
				})
			case "report:generate":
				task, taskErr = goqueue.NewTask(taskType, ReportPayload{
					ReportID:   fmt.Sprintf("RPT-%06d", taskNum),
					ReportType: []string{"daily", "weekly", "monthly"}[rand.Intn(3)],
					UserID:     fmt.Sprintf("USR-%04d", rand.Intn(100)),
				})
			case "notification:push":
				task, taskErr = goqueue.NewTask(taskType, map[string]string{
					"user_id": fmt.Sprintf("USR-%04d", rand.Intn(100)),
					"message": fmt.Sprintf("Notification #%d", taskNum),
				})
			}

			if taskErr != nil {
				logger.Error("failed to create task", "error", taskErr)
				continue
			}

			// Enqueue with options
			opts := []goqueue.EnqueueOption{
				goqueue.Queue(queue),
				goqueue.TaskPriority(priority),
			}

			// Occasionally schedule for later
			if rand.Float32() < 0.1 {
				delay := time.Duration(rand.Intn(60)) * time.Second
				opts = append(opts, goqueue.ProcessIn(delay))
				logger.Info("scheduling delayed task",
					"task_id", task.ID,
					"type", taskType,
					"delay", delay,
				)
			}

			info, err := client.Enqueue(ctx, task, opts...)
			if err != nil {
				logger.Error("failed to enqueue task", "error", err)
				continue
			}

			logger.Info("enqueued task",
				"task_id", info.ID,
				"type", taskType,
				"queue", info.Queue,
				"num", taskNum,
			)

			time.Sleep(*interval)
		}

		if !*continuous {
			break
		}

		logger.Info("batch complete, starting next batch", "total_enqueued", taskNum)
		time.Sleep(1 * time.Second)
	}

	fmt.Printf("\n  Finished enqueuing %d tasks\n\n", taskNum)
}

func randomTaskConfig() (taskType, queue string, priority goqueue.Priority) {
	// Task types with different distributions
	r := rand.Float32()
	switch {
	case r < 0.5:
		taskType = "email:send"
	case r < 0.8:
		taskType = "notification:push"
	default:
		taskType = "report:generate"
	}

	// Queue selection
	r = rand.Float32()
	switch {
	case r < 0.1:
		queue = "critical"
		priority = goqueue.PriorityCritical
	case r < 0.3:
		queue = "low"
		priority = goqueue.PriorityLow
	default:
		queue = "default"
		priority = goqueue.PriorityDefault
	}

	return
}
