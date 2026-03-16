package goqueue

import (
	"time"

	"github.com/redis/go-redis/v9"
)

// ClientConfig holds configuration for the Client.
type ClientConfig struct {
	// RedisAddr is the Redis server address (default: "localhost:6379").
	RedisAddr string
	// RedisPassword is the Redis password (default: "").
	RedisPassword string
	// RedisDB is the Redis database number (default: 0).
	RedisDB int
	// RedisClient allows providing a custom Redis client.
	RedisClient redis.UniversalClient
}

// DefaultClientConfig returns the default client configuration.
func DefaultClientConfig() ClientConfig {
	return ClientConfig{
		RedisAddr:     "localhost:6379",
		RedisPassword: "",
		RedisDB:       0,
	}
}

// ClientOption configures the Client.
type ClientOption func(*ClientConfig)

// WithRedisAddr sets the Redis server address.
func WithRedisAddr(addr string) ClientOption {
	return func(c *ClientConfig) {
		c.RedisAddr = addr
	}
}

// WithRedisPassword sets the Redis password.
func WithRedisPassword(password string) ClientOption {
	return func(c *ClientConfig) {
		c.RedisPassword = password
	}
}

// WithRedisDB sets the Redis database number.
func WithRedisDB(db int) ClientOption {
	return func(c *ClientConfig) {
		c.RedisDB = db
	}
}

// WithRedisClient sets a custom Redis client.
func WithRedisClient(client redis.UniversalClient) ClientOption {
	return func(c *ClientConfig) {
		c.RedisClient = client
	}
}

// ServerConfig holds configuration for the Server.
type ServerConfig struct {
	// RedisAddr is the Redis server address (default: "localhost:6379").
	RedisAddr string
	// RedisPassword is the Redis password (default: "").
	RedisPassword string
	// RedisDB is the Redis database number (default: 0).
	RedisDB int
	// RedisClient allows providing a custom Redis client.
	RedisClient redis.UniversalClient
	// Concurrency is the number of concurrent workers (default: 10).
	Concurrency int
	// Queues is a map of queue names to their priority weights.
	// Higher weight means more tasks from that queue will be processed.
	// Example: {"critical": 6, "default": 3, "low": 1}
	Queues map[string]int
	// ShutdownTimeout is the maximum time to wait for graceful shutdown (default: 30s).
	ShutdownTimeout time.Duration
	// PollInterval is how often to poll for tasks (default: 1s).
	PollInterval time.Duration
	// StrictPriority uses strict priority ordering instead of weighted.
	StrictPriority bool
	// LogLevel is the logging level (debug, info, warn, error).
	LogLevel string
	// RetryConfig holds retry configuration.
	RetryConfig RetryConfig
	// RateLimits holds per-queue rate limits.
	RateLimits map[string]RateLimit
}

// RetryConfig holds configuration for task retries.
type RetryConfig struct {
	// MaxRetries is the default maximum number of retries (default: 3).
	MaxRetries int
	// BaseDelay is the base delay for exponential backoff (default: 1s).
	BaseDelay time.Duration
	// MaxDelay is the maximum delay cap (default: 1h).
	MaxDelay time.Duration
}

// RateLimit defines rate limiting for a queue.
type RateLimit struct {
	// Rate is the number of tasks per interval.
	Rate int
	// Interval is the time window for the rate.
	Interval time.Duration
}

// DefaultServerConfig returns the default server configuration.
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		RedisAddr:       "localhost:6379",
		RedisPassword:   "",
		RedisDB:         0,
		Concurrency:     10,
		Queues:          map[string]int{"default": 1},
		ShutdownTimeout: 30 * time.Second,
		PollInterval:    1 * time.Second,
		StrictPriority:  false,
		LogLevel:        "info",
		RetryConfig: RetryConfig{
			MaxRetries: 3,
			BaseDelay:  1 * time.Second,
			MaxDelay:   1 * time.Hour,
		},
		RateLimits: make(map[string]RateLimit),
	}
}

// ServerOption configures the Server.
type ServerOption func(*ServerConfig)

// WithServerRedisAddr sets the Redis server address.
func WithServerRedisAddr(addr string) ServerOption {
	return func(c *ServerConfig) {
		c.RedisAddr = addr
	}
}

// WithServerRedisPassword sets the Redis password.
func WithServerRedisPassword(password string) ServerOption {
	return func(c *ServerConfig) {
		c.RedisPassword = password
	}
}

// WithServerRedisDB sets the Redis database number.
func WithServerRedisDB(db int) ServerOption {
	return func(c *ServerConfig) {
		c.RedisDB = db
	}
}

// WithServerRedisClient sets a custom Redis client.
func WithServerRedisClient(client redis.UniversalClient) ServerOption {
	return func(c *ServerConfig) {
		c.RedisClient = client
	}
}

// WithConcurrency sets the number of concurrent workers.
func WithConcurrency(n int) ServerOption {
	return func(c *ServerConfig) {
		if n > 0 {
			c.Concurrency = n
		}
	}
}

// WithQueues sets the queues to process with their priority weights.
func WithQueues(queues map[string]int) ServerOption {
	return func(c *ServerConfig) {
		c.Queues = queues
	}
}

// WithShutdownTimeout sets the graceful shutdown timeout.
func WithShutdownTimeout(timeout time.Duration) ServerOption {
	return func(c *ServerConfig) {
		c.ShutdownTimeout = timeout
	}
}

// WithPollInterval sets the task polling interval.
func WithPollInterval(interval time.Duration) ServerOption {
	return func(c *ServerConfig) {
		c.PollInterval = interval
	}
}

// WithStrictPriority enables strict priority ordering.
func WithStrictPriority(strict bool) ServerOption {
	return func(c *ServerConfig) {
		c.StrictPriority = strict
	}
}

// WithLogLevel sets the logging level.
func WithLogLevel(level string) ServerOption {
	return func(c *ServerConfig) {
		c.LogLevel = level
	}
}

// WithRetryConfig sets the retry configuration.
func WithRetryConfig(cfg RetryConfig) ServerOption {
	return func(c *ServerConfig) {
		c.RetryConfig = cfg
	}
}

// WithMaxRetries sets the maximum number of retries.
func WithMaxRetries(n int) ServerOption {
	return func(c *ServerConfig) {
		c.RetryConfig.MaxRetries = n
	}
}

// WithRateLimit sets a rate limit for a queue.
func WithRateLimit(queue string, rate int, interval time.Duration) ServerOption {
	return func(c *ServerConfig) {
		if c.RateLimits == nil {
			c.RateLimits = make(map[string]RateLimit)
		}
		c.RateLimits[queue] = RateLimit{Rate: rate, Interval: interval}
	}
}

// EnqueueConfig holds options for enqueuing a task.
type EnqueueConfig struct {
	// Queue overrides the task's queue.
	Queue string
	// Priority overrides the task's priority.
	Priority Priority
	// MaxRetries overrides the task's max retries.
	MaxRetries int
	// ProcessAt schedules the task for future processing.
	ProcessAt time.Time
	// ProcessIn schedules the task relative to now.
	ProcessIn time.Duration
	// Deadline sets the absolute deadline for task completion.
	Deadline time.Time
	// Timeout sets a relative deadline from when processing starts.
	Timeout time.Duration
	// UniqueKey enables task deduplication with this key.
	UniqueKey string
	// UniqueTTL is how long uniqueness is enforced.
	UniqueTTL time.Duration
}

// EnqueueOption configures task enqueuing.
type EnqueueOption func(*EnqueueConfig)

// Queue sets the queue for the task.
func Queue(name string) EnqueueOption {
	return func(c *EnqueueConfig) {
		c.Queue = name
	}
}

// TaskPriority sets the priority for the task.
func TaskPriority(p Priority) EnqueueOption {
	return func(c *EnqueueConfig) {
		c.Priority = p
	}
}

// MaxRetries sets the maximum retries for the task.
func MaxRetries(n int) EnqueueOption {
	return func(c *EnqueueConfig) {
		c.MaxRetries = n
	}
}

// ProcessAt schedules the task for the given time.
func ProcessAt(t time.Time) EnqueueOption {
	return func(c *EnqueueConfig) {
		c.ProcessAt = t
	}
}

// ProcessIn schedules the task relative to now.
func ProcessIn(d time.Duration) EnqueueOption {
	return func(c *EnqueueConfig) {
		c.ProcessIn = d
	}
}

// Deadline sets the absolute deadline for the task.
func Deadline(t time.Time) EnqueueOption {
	return func(c *EnqueueConfig) {
		c.Deadline = t
	}
}

// Timeout sets a relative deadline from when processing starts.
func Timeout(d time.Duration) EnqueueOption {
	return func(c *EnqueueConfig) {
		c.Timeout = d
	}
}

// Unique enables task deduplication with the given key and TTL.
func Unique(key string, ttl time.Duration) EnqueueOption {
	return func(c *EnqueueConfig) {
		c.UniqueKey = key
		c.UniqueTTL = ttl
	}
}
