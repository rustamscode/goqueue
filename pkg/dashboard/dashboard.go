// Package dashboard provides a web dashboard for GoQueue monitoring.
package dashboard

import (
	"context"
	"embed"
	"encoding/json"
	"html/template"
	"net/http"
	"time"

	"github.com/rustamscode/goqueue/pkg/broker"
	"github.com/rustamscode/goqueue/pkg/queue"
)

//go:embed templates/*
var templatesFS embed.FS

// Dashboard provides HTTP handlers for the GoQueue web UI.
type Dashboard struct {
	broker    broker.Broker
	templates *template.Template
}

// New creates a new Dashboard instance.
func New(b broker.Broker) (*Dashboard, error) {
	tmpl, err := template.ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		return nil, err
	}

	return &Dashboard{
		broker:    b,
		templates: tmpl,
	}, nil
}

// DashboardData holds data for the dashboard template.
type DashboardData struct {
	Queues         []*QueueData   `json:"queues"`
	TotalPending   int64          `json:"total_pending"`
	TotalActive    int64          `json:"total_active"`
	TotalDead      int64          `json:"total_dead"`
	TotalProcessed int64          `json:"total_processed"`
	RecentFailed   []*queue.Task  `json:"recent_failed"`
	RefreshTime    time.Time      `json:"refresh_time"`
}

// QueueData holds data for a single queue.
type QueueData struct {
	Name      string  `json:"name"`
	Pending   int64   `json:"pending"`
	Active    int64   `json:"active"`
	Completed int64   `json:"completed"`
	Failed    int64   `json:"failed"`
	Dead      int64   `json:"dead"`
	Rate      float64 `json:"rate"` // tasks per minute
}

// Handler returns the main dashboard HTTP handler.
func (d *Dashboard) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", d.handleDashboard)
	mux.HandleFunc("/api/stats", d.handleStats)
	mux.HandleFunc("/api/queues", d.handleQueues)
	mux.HandleFunc("/api/dlq", d.handleDLQ)
	mux.HandleFunc("/api/dlq/retry", d.handleDLQRetry)
	mux.HandleFunc("/api/dlq/delete", d.handleDLQDelete)
	mux.HandleFunc("/api/dlq/purge", d.handleDLQPurge)
	return mux
}

// handleDashboard renders the main dashboard page.
func (d *Dashboard) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/dashboard" {
		http.NotFound(w, r)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	data, err := d.gatherDashboardData(ctx)
	if err != nil {
		http.Error(w, "Failed to gather dashboard data", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := d.templates.ExecuteTemplate(w, "index.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleStats returns dashboard stats as JSON.
func (d *Dashboard) handleStats(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	data, err := d.gatherDashboardData(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// handleQueues returns queue list as JSON.
func (d *Dashboard) handleQueues(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	queues, err := d.broker.GetQueues(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(queues)
}

// handleDLQ returns DLQ tasks for a queue.
func (d *Dashboard) handleDLQ(w http.ResponseWriter, r *http.Request) {
	queue := r.URL.Query().Get("queue")
	if queue == "" {
		queue = "default"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	tasks, err := d.broker.GetDLQTasks(ctx, queue, 0, 100)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks)
}

// handleDLQRetry retries a task from DLQ.
func (d *Dashboard) handleDLQRetry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	queue := r.URL.Query().Get("queue")
	taskID := r.URL.Query().Get("task_id")
	if queue == "" || taskID == "" {
		http.Error(w, "queue and task_id required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := d.broker.RetryFromDLQ(ctx, queue, taskID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleDLQDelete deletes a task from DLQ.
func (d *Dashboard) handleDLQDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	queue := r.URL.Query().Get("queue")
	taskID := r.URL.Query().Get("task_id")
	if queue == "" || taskID == "" {
		http.Error(w, "queue and task_id required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := d.broker.DeleteFromDLQ(ctx, queue, taskID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleDLQPurge purges all tasks from a DLQ.
func (d *Dashboard) handleDLQPurge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	queue := r.URL.Query().Get("queue")
	if queue == "" {
		http.Error(w, "queue required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := d.broker.PurgeDLQ(ctx, queue); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// gatherDashboardData collects all data needed for the dashboard.
func (d *Dashboard) gatherDashboardData(ctx context.Context) (*DashboardData, error) {
	stats, err := d.broker.GetQueueStats(ctx)
	if err != nil {
		return nil, err
	}

	data := &DashboardData{
		Queues:      make([]*QueueData, 0, len(stats)),
		RefreshTime: time.Now(),
	}

	for name, s := range stats {
		qd := &QueueData{
			Name:      name,
			Pending:   s.Pending,
			Active:    s.Active,
			Completed: s.Completed,
			Failed:    s.Failed,
			Dead:      s.Dead,
			Rate:      s.ProcessedPerMinute,
		}
		data.Queues = append(data.Queues, qd)
		data.TotalPending += s.Pending
		data.TotalActive += s.Active
		data.TotalDead += s.Dead
		data.TotalProcessed += s.Processed
	}

	// Get recent failed tasks from DLQ
	for name := range stats {
		tasks, err := d.broker.GetDLQTasks(ctx, name, 0, 10)
		if err != nil {
			continue
		}
		data.RecentFailed = append(data.RecentFailed, tasks...)
	}

	// Limit recent failed
	if len(data.RecentFailed) > 20 {
		data.RecentFailed = data.RecentFailed[:20]
	}

	return data, nil
}
