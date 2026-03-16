// Package goqueue provides a distributed task queue for Go applications.
package goqueue

import (
	"github.com/rustamscode/goqueue/pkg/queue"
)

// Re-export types from queue package for convenience.
type (
	// Task represents a unit of work to be processed by a worker.
	Task = queue.Task
	// TaskState represents the current state of a task in its lifecycle.
	TaskState = queue.TaskState
	// Priority represents task priority levels.
	Priority = queue.Priority
	// TaskInfo contains information about an enqueued task.
	TaskInfo = queue.TaskInfo
	// QueueStats contains statistics for a queue.
	QueueStats = queue.QueueStats
)

// Re-export constants from queue package.
const (
	TaskStatePending   = queue.TaskStatePending
	TaskStateActive    = queue.TaskStateActive
	TaskStateCompleted = queue.TaskStateCompleted
	TaskStateFailed    = queue.TaskStateFailed
	TaskStateRetry     = queue.TaskStateRetry
	TaskStateDead      = queue.TaskStateDead

	PriorityLow      = queue.PriorityLow
	PriorityDefault  = queue.PriorityDefault
	PriorityHigh     = queue.PriorityHigh
	PriorityCritical = queue.PriorityCritical
)

// Re-export functions from queue package.
var (
	// NewTask creates a new task with the given type and payload.
	NewTask = queue.NewTask
	// NewTaskWithPayload creates a new task with raw JSON payload.
	NewTaskWithPayload = queue.NewTaskWithPayload
)
