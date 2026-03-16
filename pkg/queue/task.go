// Package queue provides core task queue types used across GoQueue packages.
package queue

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type TaskState string

const (
	TaskStatePending   TaskState = "pending"
	TaskStateActive    TaskState = "active"
	TaskStateCompleted TaskState = "completed"
	TaskStateFailed    TaskState = "failed"
	TaskStateRetry     TaskState = "retry"
	TaskStateDead      TaskState = "dead"
)

type Priority int

const (
	PriorityLow      Priority = 1
	PriorityDefault  Priority = 5
	PriorityHigh     Priority = 7
	PriorityCritical Priority = 10
)

type Task struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Payload    json.RawMessage `json:"payload"`
	Queue      string          `json:"queue"`
	Priority   Priority        `json:"priority"`
	State      TaskState       `json:"state"`
	MaxRetries int             `json:"max_retries"`
	RetryCount int             `json:"retry_count"`
	LastError  string          `json:"last_error,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
	ProcessAt  time.Time       `json:"process_at,omitempty"`
	StartedAt  time.Time       `json:"started_at,omitempty"`
	CompletedAt time.Time      `json:"completed_at,omitempty"`
	Deadline   time.Time       `json:"deadline,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

func NewTask(taskType string, payload any) (*Task, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	return &Task{
		ID:         uuid.New().String(),
		Type:       taskType,
		Payload:    payloadBytes,
		Queue:      "default",
		Priority:   PriorityDefault,
		State:      TaskStatePending,
		MaxRetries: 3,
		CreatedAt:  time.Now().UTC(),
		Metadata:   make(map[string]string),
	}, nil
}

func NewTaskWithPayload(taskType string, payload json.RawMessage) *Task {
	return &Task{
		ID:         uuid.New().String(),
		Type:       taskType,
		Payload:    payload,
		Queue:      "default",
		Priority:   PriorityDefault,
		State:      TaskStatePending,
		MaxRetries: 3,
		CreatedAt:  time.Now().UTC(),
		Metadata:   make(map[string]string),
	}
}

func (t *Task) UnmarshalPayload(target any) error {
	return json.Unmarshal(t.Payload, target)
}

func (t *Task) Clone() *Task {
	clone := *t
	if t.Payload != nil {
		clone.Payload = make(json.RawMessage, len(t.Payload))
		copy(clone.Payload, t.Payload)
	}
	if t.Metadata != nil {
		clone.Metadata = make(map[string]string, len(t.Metadata))
		for k, v := range t.Metadata {
			clone.Metadata[k] = v
		}
	}
	return &clone
}

func (t *Task) CanRetry() bool {
	return t.RetryCount < t.MaxRetries
}

func (t *Task) ShouldRetry() bool {
	return t.State == TaskStateFailed && t.CanRetry()
}

func (t *Task) IsTerminal() bool {
	return t.State == TaskStateCompleted || t.State == TaskStateDead
}

func (t *Task) IsDelayed() bool {
	return !t.ProcessAt.IsZero() && t.ProcessAt.After(time.Now())
}

func (t *Task) IsPastDeadline() bool {
	return !t.Deadline.IsZero() && time.Now().After(t.Deadline)
}

func (t *Task) SetMetadata(key, value string) {
	if t.Metadata == nil {
		t.Metadata = make(map[string]string)
	}
	t.Metadata[key] = value
}

func (t *Task) GetMetadata(key string) string {
	if t.Metadata == nil {
		return ""
	}
	return t.Metadata[key]
}

func (t *Task) Validate() error {
	if t.ID == "" {
		return fmt.Errorf("task ID is required")
	}
	if t.Type == "" {
		return fmt.Errorf("task type is required")
	}
	if t.Queue == "" {
		return fmt.Errorf("task queue is required")
	}
	return nil
}

func (t *Task) MarshalBinary() ([]byte, error) {
	return json.Marshal(t)
}

func (t *Task) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, t)
}

type TaskInfo struct {
	ID        string
	Queue     string
	State     TaskState
	ProcessAt time.Time
}

type QueueStats struct {
	Name               string  `json:"name"`
	Pending            int64   `json:"pending"`
	Active             int64   `json:"active"`
	Completed          int64   `json:"completed"`
	Failed             int64   `json:"failed"`
	Dead               int64   `json:"dead"`
	Processed          int64   `json:"processed"`
	ProcessedPerMinute float64 `json:"processed_per_minute"`
}
