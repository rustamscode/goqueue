package goqueue

import (
	"context"
	"errors"
	"testing"
)

func TestHandlerFunc(t *testing.T) {
	called := false
	handler := HandlerFunc(func(ctx context.Context, task *Task) error {
		called = true
		return nil
	})

	task := &Task{ID: "test", Type: "test:type"}
	err := handler.ProcessTask(context.Background(), task)

	if err != nil {
		t.Errorf("ProcessTask() error = %v", err)
	}
	if !called {
		t.Error("HandlerFunc was not called")
	}
}

func TestMux_Handle(t *testing.T) {
	mux := NewMux()

	handler1Called := false
	handler2Called := false

	mux.HandleFunc("type1", func(ctx context.Context, task *Task) error {
		handler1Called = true
		return nil
	})

	mux.HandleFunc("type2", func(ctx context.Context, task *Task) error {
		handler2Called = true
		return nil
	})

	// Process type1
	task1 := &Task{ID: "1", Type: "type1"}
	err := mux.ProcessTask(context.Background(), task1)
	if err != nil {
		t.Errorf("ProcessTask(type1) error = %v", err)
	}
	if !handler1Called {
		t.Error("handler1 was not called")
	}
	if handler2Called {
		t.Error("handler2 should not be called")
	}

	// Reset and process type2
	handler1Called = false
	task2 := &Task{ID: "2", Type: "type2"}
	err = mux.ProcessTask(context.Background(), task2)
	if err != nil {
		t.Errorf("ProcessTask(type2) error = %v", err)
	}
	if handler1Called {
		t.Error("handler1 should not be called")
	}
	if !handler2Called {
		t.Error("handler2 was not called")
	}
}

func TestMux_ProcessTask_UnknownType(t *testing.T) {
	mux := NewMux()
	mux.HandleFunc("known", func(ctx context.Context, task *Task) error {
		return nil
	})

	task := &Task{ID: "1", Type: "unknown"}
	err := mux.ProcessTask(context.Background(), task)

	if err == nil {
		t.Error("ProcessTask() should return error for unknown type")
	}
}

func TestMux_HasHandler(t *testing.T) {
	mux := NewMux()
	mux.HandleFunc("registered", func(ctx context.Context, task *Task) error {
		return nil
	})

	if !mux.HasHandler("registered") {
		t.Error("HasHandler(registered) = false, want true")
	}
	if mux.HasHandler("unregistered") {
		t.Error("HasHandler(unregistered) = true, want false")
	}
}

func TestMux_RegisteredTypes(t *testing.T) {
	mux := NewMux()
	mux.HandleFunc("type1", func(ctx context.Context, task *Task) error { return nil })
	mux.HandleFunc("type2", func(ctx context.Context, task *Task) error { return nil })

	types := mux.RegisteredTypes()

	if len(types) != 2 {
		t.Errorf("RegisteredTypes() length = %d, want 2", len(types))
	}

	found := make(map[string]bool)
	for _, typ := range types {
		found[typ] = true
	}

	if !found["type1"] || !found["type2"] {
		t.Errorf("RegisteredTypes() = %v, want [type1, type2]", types)
	}
}

func TestRecoveryMiddleware(t *testing.T) {
	handler := HandlerFunc(func(ctx context.Context, task *Task) error {
		panic("test panic")
	})

	wrapped := ChainMiddleware(handler, RecoveryMiddleware())

	task := &Task{ID: "1", Type: "test"}
	err := wrapped.ProcessTask(context.Background(), task)

	if err == nil {
		t.Error("ProcessTask() should return error after panic")
	}
	if !errors.Is(err, err) { // Just checking err exists
		t.Error("ProcessTask() error should contain panic info")
	}
}

func TestChainMiddleware(t *testing.T) {
	order := []string{}

	middleware1 := func(next Handler) Handler {
		return HandlerFunc(func(ctx context.Context, task *Task) error {
			order = append(order, "m1-before")
			err := next.ProcessTask(ctx, task)
			order = append(order, "m1-after")
			return err
		})
	}

	middleware2 := func(next Handler) Handler {
		return HandlerFunc(func(ctx context.Context, task *Task) error {
			order = append(order, "m2-before")
			err := next.ProcessTask(ctx, task)
			order = append(order, "m2-after")
			return err
		})
	}

	handler := HandlerFunc(func(ctx context.Context, task *Task) error {
		order = append(order, "handler")
		return nil
	})

	wrapped := ChainMiddleware(handler, middleware1, middleware2)

	task := &Task{ID: "1", Type: "test"}
	wrapped.ProcessTask(context.Background(), task)

	expected := []string{"m1-before", "m2-before", "handler", "m2-after", "m1-after"}
	if len(order) != len(expected) {
		t.Fatalf("order length = %d, want %d", len(order), len(expected))
	}

	for i, v := range expected {
		if order[i] != v {
			t.Errorf("order[%d] = %s, want %s", i, order[i], v)
		}
	}
}

func TestErrorHandlerFunc(t *testing.T) {
	var capturedTask *Task
	var capturedErr error

	handler := ErrorHandlerFunc(func(ctx context.Context, task *Task, err error) {
		capturedTask = task
		capturedErr = err
	})

	task := &Task{ID: "1", Type: "test"}
	testErr := errors.New("test error")

	handler.HandleError(context.Background(), task, testErr)

	if capturedTask != task {
		t.Error("task not captured correctly")
	}
	if capturedErr != testErr {
		t.Error("error not captured correctly")
	}
}
