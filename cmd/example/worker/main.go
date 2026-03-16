// Example GoQueue worker that processes tasks.
package main

import (
	"context"
	"encoding/json"
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
		concurrency   = flag.Int("concurrency", 10, "Number of concurrent workers")
		logLevel      = flag.String("log-level", "info", "Log level")
	)
	flag.Parse()

	// Setup logger
	var level slog.Level
	switch *logLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	// Create server
	server, err := goqueue.NewServer(
		goqueue.WithServerRedisAddr(*redisAddr),
		goqueue.WithServerRedisPassword(*redisPassword),
		goqueue.WithConcurrency(*concurrency),
		goqueue.WithQueues(map[string]int{
			"critical": 6,
			"default":  3,
			"low":      1,
		}),
		goqueue.WithLogLevel(*logLevel),
		goqueue.WithMaxRetries(3),
		goqueue.WithShutdownTimeout(30*time.Second),
	)
	if err != nil {
		logger.Error("failed to create server", "error", err)
		os.Exit(1)
	}

	// Register handlers
	server.HandleFunc("email:send", handleEmailTask)
	server.HandleFunc("report:generate", handleReportTask)
	server.HandleFunc("notification:push", handleNotificationTask)

	fmt.Printf("\n  GoQueue Worker started\n")
	fmt.Printf("  Concurrency: %d\n", *concurrency)
	fmt.Printf("  Queues: critical (6), default (3), low (1)\n")
	fmt.Printf("  Task types: email:send, report:generate, notification:push\n\n")

	// Start processing
	if err := server.Run(); err != nil {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}

func handleEmailTask(ctx context.Context, task *goqueue.Task) error {
	var payload EmailPayload
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		return fmt.Errorf("invalid payload: %w", err)
	}

	slog.Info("sending email",
		"task_id", task.ID,
		"to", payload.To,
		"subject", payload.Subject,
	)

	// Simulate email sending
	time.Sleep(time.Duration(100+rand.Intn(200)) * time.Millisecond)

	// Simulate occasional failures
	if rand.Float32() < 0.1 {
		return fmt.Errorf("SMTP connection failed")
	}

	slog.Info("email sent successfully", "task_id", task.ID)
	return nil
}

func handleReportTask(ctx context.Context, task *goqueue.Task) error {
	var payload ReportPayload
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		return fmt.Errorf("invalid payload: %w", err)
	}

	slog.Info("generating report",
		"task_id", task.ID,
		"report_id", payload.ReportID,
		"type", payload.ReportType,
	)

	// Simulate report generation (longer task)
	time.Sleep(time.Duration(500+rand.Intn(1000)) * time.Millisecond)

	// Check for cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Simulate occasional failures
	if rand.Float32() < 0.05 {
		return fmt.Errorf("database query timeout")
	}

	slog.Info("report generated", "task_id", task.ID, "report_id", payload.ReportID)
	return nil
}

func handleNotificationTask(ctx context.Context, task *goqueue.Task) error {
	slog.Info("pushing notification", "task_id", task.ID)

	// Simulate push notification
	time.Sleep(time.Duration(50+rand.Intn(100)) * time.Millisecond)

	slog.Info("notification pushed", "task_id", task.ID)
	return nil
}
