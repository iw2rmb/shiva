package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/iw2rmb/shiva/internal/openapi"
)

const (
	defaultHTTPAddr              = ":8080"
	defaultWorkerConcurrency     = 4
	defaultShutdownTimeoutSecond = 15
	defaultOutboundTimeoutSecond = 10
	defaultLogLevel              = "info"
	defaultIngressBodyLimitBytes = 1024 * 1024
	defaultIngressRateLimitMax   = 120
	defaultIngressRateLimitSec   = 60
	defaultMetricsPath           = "/internal/metrics"
)

type Config struct {
	HTTPAddr             string
	DatabaseURL          string
	GitLabBaseURL        string
	GitLabToken          string
	GitLabWebhookSecret  string
	TenantKey            string
	WorkerConcurrency    int
	ShutdownTimeout      time.Duration
	OutboundTimeout      time.Duration
	LogLevel             slog.Level
	OpenAPIPathGlobs     []string
	OpenAPIRefMaxFetches int
	IngressBodyLimit     int
	IngressRateLimitMax  int
	IngressRateLimit     time.Duration
	MetricsPath          string
	TracingEnabled       bool
	TracingStdout        bool
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:             envValue("SHIVA_HTTP_ADDR", defaultHTTPAddr),
		DatabaseURL:          strings.TrimSpace(os.Getenv("SHIVA_DATABASE_URL")),
		GitLabBaseURL:        strings.TrimSpace(os.Getenv("SHIVA_GITLAB_BASE_URL")),
		GitLabToken:          strings.TrimSpace(os.Getenv("SHIVA_GITLAB_TOKEN")),
		GitLabWebhookSecret:  strings.TrimSpace(os.Getenv("SHIVA_GITLAB_WEBHOOK_SECRET")),
		TenantKey:            envValue("SHIVA_TENANT_KEY", "default"),
		WorkerConcurrency:    defaultWorkerConcurrency,
		ShutdownTimeout:      time.Duration(defaultShutdownTimeoutSecond) * time.Second,
		OutboundTimeout:      time.Duration(defaultOutboundTimeoutSecond) * time.Second,
		LogLevel:             slog.LevelInfo,
		OpenAPIPathGlobs:     openapi.DefaultIncludeGlobs(),
		OpenAPIRefMaxFetches: openapi.DefaultMaxFetches,
		IngressBodyLimit:     defaultIngressBodyLimitBytes,
		IngressRateLimitMax:  defaultIngressRateLimitMax,
		IngressRateLimit:     time.Duration(defaultIngressRateLimitSec) * time.Second,
		MetricsPath:          defaultMetricsPath,
		TracingEnabled:       true,
		TracingStdout:        false,
	}

	if rawLevel, ok := os.LookupEnv("SHIVA_LOG_LEVEL"); ok {
		level, err := parseLogLevel(rawLevel)
		if err != nil {
			return Config{}, err
		}
		cfg.LogLevel = level
	}

	if rawConcurrency, ok := os.LookupEnv("SHIVA_WORKER_CONCURRENCY"); ok {
		concurrency, err := strconv.Atoi(strings.TrimSpace(rawConcurrency))
		if err != nil {
			return Config{}, fmt.Errorf("invalid SHIVA_WORKER_CONCURRENCY: %w", err)
		}
		if concurrency < 1 {
			return Config{}, errors.New("SHIVA_WORKER_CONCURRENCY must be at least 1")
		}
		cfg.WorkerConcurrency = concurrency
	}

	if rawTimeout, ok := os.LookupEnv("SHIVA_SHUTDOWN_TIMEOUT_SECONDS"); ok {
		timeoutSeconds, err := strconv.ParseInt(strings.TrimSpace(rawTimeout), 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("invalid SHIVA_SHUTDOWN_TIMEOUT_SECONDS: %w", err)
		}
		if timeoutSeconds < 1 {
			return Config{}, errors.New("SHIVA_SHUTDOWN_TIMEOUT_SECONDS must be at least 1")
		}
		cfg.ShutdownTimeout = time.Duration(timeoutSeconds) * time.Second
	}

	if rawTimeout, ok := os.LookupEnv("SHIVA_OUTBOUND_TIMEOUT_SECONDS"); ok {
		timeoutSeconds, err := strconv.ParseInt(strings.TrimSpace(rawTimeout), 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("invalid SHIVA_OUTBOUND_TIMEOUT_SECONDS: %w", err)
		}
		if timeoutSeconds < 1 {
			return Config{}, errors.New("SHIVA_OUTBOUND_TIMEOUT_SECONDS must be at least 1")
		}
		cfg.OutboundTimeout = time.Duration(timeoutSeconds) * time.Second
	}

	if rawGlobs, ok := os.LookupEnv("SHIVA_OPENAPI_PATH_GLOBS"); ok {
		globs, err := parseCommaSeparatedValues(rawGlobs)
		if err != nil {
			return Config{}, fmt.Errorf("invalid SHIVA_OPENAPI_PATH_GLOBS: %w", err)
		}
		cfg.OpenAPIPathGlobs = globs
	}

	if rawMaxFetches, ok := os.LookupEnv("SHIVA_OPENAPI_REF_MAX_FETCHES"); ok {
		maxFetches, err := strconv.Atoi(strings.TrimSpace(rawMaxFetches))
		if err != nil {
			return Config{}, fmt.Errorf("invalid SHIVA_OPENAPI_REF_MAX_FETCHES: %w", err)
		}
		if maxFetches < 1 {
			return Config{}, errors.New("SHIVA_OPENAPI_REF_MAX_FETCHES must be at least 1")
		}
		cfg.OpenAPIRefMaxFetches = maxFetches
	}

	if rawBodyLimit, ok := os.LookupEnv("SHIVA_INGRESS_BODY_LIMIT_BYTES"); ok {
		bodyLimit, err := strconv.Atoi(strings.TrimSpace(rawBodyLimit))
		if err != nil {
			return Config{}, fmt.Errorf("invalid SHIVA_INGRESS_BODY_LIMIT_BYTES: %w", err)
		}
		if bodyLimit < 1 {
			return Config{}, errors.New("SHIVA_INGRESS_BODY_LIMIT_BYTES must be at least 1")
		}
		cfg.IngressBodyLimit = bodyLimit
	}

	if rawRateLimitMax, ok := os.LookupEnv("SHIVA_INGRESS_RATE_LIMIT_MAX"); ok {
		rateLimitMax, err := strconv.Atoi(strings.TrimSpace(rawRateLimitMax))
		if err != nil {
			return Config{}, fmt.Errorf("invalid SHIVA_INGRESS_RATE_LIMIT_MAX: %w", err)
		}
		if rateLimitMax < 1 {
			return Config{}, errors.New("SHIVA_INGRESS_RATE_LIMIT_MAX must be at least 1")
		}
		cfg.IngressRateLimitMax = rateLimitMax
	}

	if rawRateLimitSec, ok := os.LookupEnv("SHIVA_INGRESS_RATE_LIMIT_WINDOW_SECONDS"); ok {
		rateLimitSec, err := strconv.Atoi(strings.TrimSpace(rawRateLimitSec))
		if err != nil {
			return Config{}, fmt.Errorf("invalid SHIVA_INGRESS_RATE_LIMIT_WINDOW_SECONDS: %w", err)
		}
		if rateLimitSec < 1 {
			return Config{}, errors.New("SHIVA_INGRESS_RATE_LIMIT_WINDOW_SECONDS must be at least 1")
		}
		cfg.IngressRateLimit = time.Duration(rateLimitSec) * time.Second
	}

	if rawMetricsPath, ok := os.LookupEnv("SHIVA_METRICS_PATH"); ok {
		metricsPath := strings.TrimSpace(rawMetricsPath)
		if metricsPath == "" {
			return Config{}, errors.New("SHIVA_METRICS_PATH must not be empty")
		}
		if !strings.HasPrefix(metricsPath, "/") {
			return Config{}, errors.New("SHIVA_METRICS_PATH must start with /")
		}
		cfg.MetricsPath = metricsPath
	}

	if rawTracingEnabled, ok := os.LookupEnv("SHIVA_TRACING_ENABLED"); ok {
		tracingEnabled, err := strconv.ParseBool(strings.TrimSpace(rawTracingEnabled))
		if err != nil {
			return Config{}, fmt.Errorf("invalid SHIVA_TRACING_ENABLED: %w", err)
		}
		cfg.TracingEnabled = tracingEnabled
	}

	if rawTracingStdout, ok := os.LookupEnv("SHIVA_TRACING_STDOUT"); ok {
		tracingStdout, err := strconv.ParseBool(strings.TrimSpace(rawTracingStdout))
		if err != nil {
			return Config{}, fmt.Errorf("invalid SHIVA_TRACING_STDOUT: %w", err)
		}
		cfg.TracingStdout = tracingStdout
	}

	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = defaultHTTPAddr
	}
	if cfg.TenantKey == "" {
		return Config{}, errors.New("SHIVA_TENANT_KEY must not be empty")
	}

	if strings.TrimSpace(cfg.DatabaseURL) != "" && !strings.HasPrefix(strings.ToLower(cfg.DatabaseURL), "postgres://") {
		if !strings.HasPrefix(strings.ToLower(cfg.DatabaseURL), "postgresql://") {
			return Config{}, errors.New("SHIVA_DATABASE_URL must start with postgres:// or postgresql://")
		}
	}

	return cfg, nil
}

func parseLogLevel(rawLevel string) (slog.Level, error) {
	normalized := strings.ToLower(strings.TrimSpace(rawLevel))
	if normalized == "" {
		normalized = defaultLogLevel
	}

	switch normalized {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unsupported log level: %s", rawLevel)
	}
}

func NewLogger(level slog.Level) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level, AddSource: false})
	return slog.New(handler)
}

func envValue(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return strings.TrimSpace(value)
	}
	return fallback
}

func parseCommaSeparatedValues(raw string) ([]string, error) {
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		values = append(values, trimmed)
	}
	if len(values) == 0 {
		return nil, errors.New("must contain at least one non-empty glob")
	}
	return values, nil
}
