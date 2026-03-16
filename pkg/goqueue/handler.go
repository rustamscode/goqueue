package goqueue

import (
	"context"
	"fmt"
)

// Handler processes tasks of a specific type.
type Handler interface {
	// ProcessTask processes a task and returns an error if processing fails.
	// The context may be cancelled if the worker is shutting down or if the
	// task deadline is exceeded.
	ProcessTask(ctx context.Context, task *Task) error
}

// HandlerFunc is an adapter to allow using ordinary functions as handlers.
type HandlerFunc func(ctx context.Context, task *Task) error

// ProcessTask calls the underlying function.
func (f HandlerFunc) ProcessTask(ctx context.Context, task *Task) error {
	return f(ctx, task)
}

// MiddlewareFunc is a function that wraps a handler to add behavior.
type MiddlewareFunc func(Handler) Handler

// ChainMiddleware chains multiple middleware functions together.
// Middleware is applied in the order provided (first middleware wraps the handler last).
func ChainMiddleware(handler Handler, middlewares ...MiddlewareFunc) Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}
	return handler
}

// RecoveryMiddleware catches panics in handlers and converts them to errors.
func RecoveryMiddleware() MiddlewareFunc {
	return func(next Handler) Handler {
		return HandlerFunc(func(ctx context.Context, task *Task) (err error) {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("panic recovered: %v", r)
				}
			}()
			return next.ProcessTask(ctx, task)
		})
	}
}

// ErrorHandler is called when task processing fails.
type ErrorHandler interface {
	// HandleError is called when a task fails to process.
	HandleError(ctx context.Context, task *Task, err error)
}

// ErrorHandlerFunc is an adapter for using functions as error handlers.
type ErrorHandlerFunc func(ctx context.Context, task *Task, err error)

// HandleError calls the underlying function.
func (f ErrorHandlerFunc) HandleError(ctx context.Context, task *Task, err error) {
	f(ctx, task, err)
}

// Mux is a task handler multiplexer that routes tasks to handlers based on type.
type Mux struct {
	handlers map[string]Handler
}

// NewMux creates a new task handler multiplexer.
func NewMux() *Mux {
	return &Mux{
		handlers: make(map[string]Handler),
	}
}

// Handle registers a handler for tasks of the given type.
func (m *Mux) Handle(taskType string, handler Handler) {
	m.handlers[taskType] = handler
}

// HandleFunc registers a handler function for tasks of the given type.
func (m *Mux) HandleFunc(taskType string, handler func(ctx context.Context, task *Task) error) {
	m.handlers[taskType] = HandlerFunc(handler)
}

// ProcessTask routes the task to the appropriate handler based on task type.
func (m *Mux) ProcessTask(ctx context.Context, task *Task) error {
	handler, ok := m.handlers[task.Type]
	if !ok {
		return fmt.Errorf("no handler registered for task type: %s", task.Type)
	}
	return handler.ProcessTask(ctx, task)
}

// HasHandler returns true if a handler is registered for the given task type.
func (m *Mux) HasHandler(taskType string) bool {
	_, ok := m.handlers[taskType]
	return ok
}

// RegisteredTypes returns a list of all registered task types.
func (m *Mux) RegisteredTypes() []string {
	types := make([]string, 0, len(m.handlers))
	for t := range m.handlers {
		types = append(types, t)
	}
	return types
}
