package goqueue

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewTask(t *testing.T) {
	payload := map[string]string{"key": "value"}
	task, err := NewTask("test:task", payload)
	if err != nil {
		t.Fatalf("NewTask() error = %v", err)
	}

	if task.ID == "" {
		t.Error("NewTask() ID should not be empty")
	}
	if task.Type != "test:task" {
		t.Errorf("NewTask() Type = %v, want test:task", task.Type)
	}
	if task.Queue != "default" {
		t.Errorf("NewTask() Queue = %v, want default", task.Queue)
	}
	if task.Priority != PriorityDefault {
		t.Errorf("NewTask() Priority = %v, want %v", task.Priority, PriorityDefault)
	}
	if task.State != TaskStatePending {
		t.Errorf("NewTask() State = %v, want %v", task.State, TaskStatePending)
	}
	if task.MaxRetries != 3 {
		t.Errorf("NewTask() MaxRetries = %v, want 3", task.MaxRetries)
	}
	if task.CreatedAt.IsZero() {
		t.Error("NewTask() CreatedAt should not be zero")
	}
}

func TestTask_UnmarshalPayload(t *testing.T) {
	type Payload struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	original := Payload{Name: "test", Count: 42}
	task, err := NewTask("test:task", original)
	if err != nil {
		t.Fatalf("NewTask() error = %v", err)
	}

	var decoded Payload
	if err := task.UnmarshalPayload(&decoded); err != nil {
		t.Fatalf("UnmarshalPayload() error = %v", err)
	}

	if decoded.Name != original.Name || decoded.Count != original.Count {
		t.Errorf("UnmarshalPayload() = %+v, want %+v", decoded, original)
	}
}

func TestTask_Clone(t *testing.T) {
	task, _ := NewTask("test:task", map[string]string{"key": "value"})
	task.SetMetadata("meta1", "value1")

	clone := task.Clone()

	if clone.ID != task.ID {
		t.Errorf("Clone() ID = %v, want %v", clone.ID, task.ID)
	}

	// Modify original metadata
	task.SetMetadata("meta1", "modified")

	// Clone should be unaffected
	if clone.GetMetadata("meta1") != "value1" {
		t.Errorf("Clone() metadata should be independent, got %v", clone.GetMetadata("meta1"))
	}
}

func TestTask_CanRetry(t *testing.T) {
	tests := []struct {
		name       string
		retryCount int
		maxRetries int
		want       bool
	}{
		{"no retries yet", 0, 3, true},
		{"some retries", 1, 3, true},
		{"at max", 3, 3, false},
		{"over max", 4, 3, false},
		{"zero max retries", 0, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &Task{
				RetryCount: tt.retryCount,
				MaxRetries: tt.maxRetries,
			}
			if got := task.CanRetry(); got != tt.want {
				t.Errorf("CanRetry() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTask_ShouldRetry(t *testing.T) {
	tests := []struct {
		name       string
		state      TaskState
		retryCount int
		maxRetries int
		want       bool
	}{
		{"failed and can retry", TaskStateFailed, 1, 3, true},
		{"failed but exhausted", TaskStateFailed, 3, 3, false},
		{"completed", TaskStateCompleted, 0, 3, false},
		{"pending", TaskStatePending, 0, 3, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &Task{
				State:      tt.state,
				RetryCount: tt.retryCount,
				MaxRetries: tt.maxRetries,
			}
			if got := task.ShouldRetry(); got != tt.want {
				t.Errorf("ShouldRetry() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTask_IsTerminal(t *testing.T) {
	tests := []struct {
		state TaskState
		want  bool
	}{
		{TaskStatePending, false},
		{TaskStateActive, false},
		{TaskStateFailed, false},
		{TaskStateRetry, false},
		{TaskStateCompleted, true},
		{TaskStateDead, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			task := &Task{State: tt.state}
			if got := task.IsTerminal(); got != tt.want {
				t.Errorf("IsTerminal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTask_IsDelayed(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		processAt time.Time
		want      bool
	}{
		{"zero time", time.Time{}, false},
		{"past", now.Add(-1 * time.Hour), false},
		{"future", now.Add(1 * time.Hour), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &Task{ProcessAt: tt.processAt}
			if got := task.IsDelayed(); got != tt.want {
				t.Errorf("IsDelayed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTask_IsPastDeadline(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		deadline time.Time
		want     bool
	}{
		{"zero deadline", time.Time{}, false},
		{"future deadline", now.Add(1 * time.Hour), false},
		{"past deadline", now.Add(-1 * time.Hour), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &Task{Deadline: tt.deadline}
			if got := task.IsPastDeadline(); got != tt.want {
				t.Errorf("IsPastDeadline() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTask_Metadata(t *testing.T) {
	task := &Task{}

	// Get from nil metadata
	if got := task.GetMetadata("key"); got != "" {
		t.Errorf("GetMetadata() from nil = %v, want empty", got)
	}

	// Set initializes metadata
	task.SetMetadata("key1", "value1")
	if got := task.GetMetadata("key1"); got != "value1" {
		t.Errorf("GetMetadata() = %v, want value1", got)
	}

	// Overwrite
	task.SetMetadata("key1", "value2")
	if got := task.GetMetadata("key1"); got != "value2" {
		t.Errorf("GetMetadata() after overwrite = %v, want value2", got)
	}
}

func TestTask_Validate(t *testing.T) {
	tests := []struct {
		name    string
		task    *Task
		wantErr bool
	}{
		{"valid", &Task{ID: "123", Type: "test", Queue: "default"}, false},
		{"missing ID", &Task{ID: "", Type: "test", Queue: "default"}, true},
		{"missing Type", &Task{ID: "123", Type: "", Queue: "default"}, true},
		{"missing Queue", &Task{ID: "123", Type: "test", Queue: ""}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.task.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestTask_MarshalBinary(t *testing.T) {
	task, _ := NewTask("test:task", map[string]string{"key": "value"})

	data, err := task.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}

	var decoded Task
	if err := decoded.UnmarshalBinary(data); err != nil {
		t.Fatalf("UnmarshalBinary() error = %v", err)
	}

	if decoded.ID != task.ID {
		t.Errorf("ID = %v, want %v", decoded.ID, task.ID)
	}
	if decoded.Type != task.Type {
		t.Errorf("Type = %v, want %v", decoded.Type, task.Type)
	}
}

func BenchmarkNewTask(b *testing.B) {
	payload := map[string]string{"key": "value"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		NewTask("test:task", payload)
	}
}

func BenchmarkTaskMarshal(b *testing.B) {
	task, _ := NewTask("test:task", map[string]string{"key": "value"})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		json.Marshal(task)
	}
}
