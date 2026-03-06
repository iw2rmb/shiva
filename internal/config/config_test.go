package config

import (
	"os"
	"testing"
	"time"

	"github.com/iw2rmb/shiva/internal/openapi"
)

func TestLoad_DefaultValues(t *testing.T) {
	setRequiredConfigEnv(t)

	t.Cleanup(func() {
		for _, name := range []string{
			"SHIVA_HTTP_ADDR",
			"SHIVA_DATABASE_URL",
			"SHIVA_LOG_LEVEL",
			"SHIVA_WORKER_CONCURRENCY",
			"SHIVA_SHUTDOWN_TIMEOUT_SECONDS",
			"SHIVA_OUTBOUND_TIMEOUT_SECONDS",
			"SHIVA_GITLAB_BASE_URL",
			"SHIVA_GITLAB_TOKEN",
			"SHIVA_GITLAB_WEBHOOK_SECRET",
			"SHIVA_TENANT_KEY",
			"SHIVA_OPENAPI_PATH_GLOBS",
			"SHIVA_OPENAPI_REF_MAX_FETCHES",
			"SHIVA_OPENAPI_BOOTSTRAP_FETCH_CONCURRENCY",
			"SHIVA_OPENAPI_BOOTSTRAP_SNIFF_BYTES",
			"SHIVA_INGRESS_BODY_LIMIT_BYTES",
			"SHIVA_INGRESS_RATE_LIMIT_MAX",
			"SHIVA_INGRESS_RATE_LIMIT_WINDOW_SECONDS",
			"SHIVA_METRICS_PATH",
			"SHIVA_TRACING_ENABLED",
			"SHIVA_TRACING_STDOUT",
		} {
			os.Unsetenv(name)
		}
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("expected default http addr :8080, got %q", cfg.HTTPAddr)
	}
	if cfg.WorkerConcurrency != 4 {
		t.Fatalf("expected default worker concurrency 4, got %d", cfg.WorkerConcurrency)
	}
	if cfg.ShutdownTimeout != 15*time.Second {
		t.Fatalf("expected default shutdown timeout 15s, got %s", cfg.ShutdownTimeout)
	}
	if cfg.OutboundTimeout != 10*time.Second {
		t.Fatalf("expected default outbound timeout 10s, got %s", cfg.OutboundTimeout)
	}
	if cfg.TenantKey != "default" {
		t.Fatalf("expected default tenant key \"default\", got %q", cfg.TenantKey)
	}
	if len(cfg.OpenAPIPathGlobs) == 0 {
		t.Fatalf("expected default openapi path globs to be configured")
	}
	if cfg.OpenAPIRefMaxFetches != 128 {
		t.Fatalf("expected default openapi ref max fetches 128, got %d", cfg.OpenAPIRefMaxFetches)
	}
	if cfg.OpenAPIBootstrapFetchConcurrency != openapi.DefaultBootstrapFetchConcurrency {
		t.Fatalf(
			"expected default openapi bootstrap fetch concurrency %d, got %d",
			openapi.DefaultBootstrapFetchConcurrency,
			cfg.OpenAPIBootstrapFetchConcurrency,
		)
	}
	if cfg.OpenAPIBootstrapSniffBytes != openapi.DefaultBootstrapSniffBytes {
		t.Fatalf(
			"expected default openapi bootstrap sniff bytes %d, got %d",
			openapi.DefaultBootstrapSniffBytes,
			cfg.OpenAPIBootstrapSniffBytes,
		)
	}
	if cfg.IngressBodyLimit != 1024*1024 {
		t.Fatalf("expected default ingress body limit 1048576, got %d", cfg.IngressBodyLimit)
	}
	if cfg.IngressRateLimitMax != 120 {
		t.Fatalf("expected default ingress rate limit max 120, got %d", cfg.IngressRateLimitMax)
	}
	if cfg.IngressRateLimit != 60*time.Second {
		t.Fatalf("expected default ingress rate limit 60s, got %s", cfg.IngressRateLimit)
	}
	if cfg.MetricsPath != "/internal/metrics" {
		t.Fatalf("expected default metrics path /internal/metrics, got %q", cfg.MetricsPath)
	}
	if !cfg.TracingEnabled {
		t.Fatalf("expected tracing enabled by default")
	}
	if cfg.TracingStdout {
		t.Fatalf("expected tracing stdout disabled by default")
	}
}

func TestLoad_InvalidWorkerConcurrency(t *testing.T) {
	setRequiredConfigEnv(t)

	t.Setenv("SHIVA_WORKER_CONCURRENCY", "zero")
	_, err := Load()
	if err == nil {
		t.Fatalf("expected error for invalid worker concurrency")
	}
}

func TestLoad_RejectsEmptyDatabaseURL(t *testing.T) {
	t.Setenv("SHIVA_DATABASE_URL", "   ")

	_, err := Load()
	if err == nil {
		t.Fatalf("expected error for empty database url")
	}
	if err.Error() != "SHIVA_DATABASE_URL must not be empty" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_RejectsEmptyTenantKey(t *testing.T) {
	setRequiredConfigEnv(t)

	t.Setenv("SHIVA_TENANT_KEY", "  ")
	_, err := Load()
	if err == nil {
		t.Fatalf("expected error for empty tenant key")
	}
}

func TestLoad_OpenAPIConfig(t *testing.T) {
	setRequiredConfigEnv(t)

	t.Setenv("SHIVA_OPENAPI_PATH_GLOBS", "specs/**/*.yaml,docs/swagger*.yml")
	t.Setenv("SHIVA_OPENAPI_REF_MAX_FETCHES", "64")
	t.Setenv("SHIVA_OPENAPI_BOOTSTRAP_FETCH_CONCURRENCY", "6")
	t.Setenv("SHIVA_OPENAPI_BOOTSTRAP_SNIFF_BYTES", "8192")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	expectedGlobs := []string{"specs/**/*.yaml", "docs/swagger*.yml"}
	if len(cfg.OpenAPIPathGlobs) != len(expectedGlobs) {
		t.Fatalf("expected %d globs, got %d", len(expectedGlobs), len(cfg.OpenAPIPathGlobs))
	}
	for i := range expectedGlobs {
		if cfg.OpenAPIPathGlobs[i] != expectedGlobs[i] {
			t.Fatalf("expected glob %d to be %q, got %q", i, expectedGlobs[i], cfg.OpenAPIPathGlobs[i])
		}
	}
	if cfg.OpenAPIRefMaxFetches != 64 {
		t.Fatalf("expected OpenAPIRefMaxFetches=64, got %d", cfg.OpenAPIRefMaxFetches)
	}
	if cfg.OpenAPIBootstrapFetchConcurrency != 6 {
		t.Fatalf("expected OpenAPIBootstrapFetchConcurrency=6, got %d", cfg.OpenAPIBootstrapFetchConcurrency)
	}
	if cfg.OpenAPIBootstrapSniffBytes != 8192 {
		t.Fatalf("expected OpenAPIBootstrapSniffBytes=8192, got %d", cfg.OpenAPIBootstrapSniffBytes)
	}
}

func TestLoad_OpenAPIBootstrapConfigValidation(t *testing.T) {
	setRequiredConfigEnv(t)

	testCases := []struct {
		name    string
		envKey  string
		envVal  string
		wantErr string
	}{
		{
			name:    "fetch concurrency must be positive",
			envKey:  "SHIVA_OPENAPI_BOOTSTRAP_FETCH_CONCURRENCY",
			envVal:  "0",
			wantErr: "SHIVA_OPENAPI_BOOTSTRAP_FETCH_CONCURRENCY must be at least 1",
		},
		{
			name:    "sniff bytes must be positive",
			envKey:  "SHIVA_OPENAPI_BOOTSTRAP_SNIFF_BYTES",
			envVal:  "0",
			wantErr: "SHIVA_OPENAPI_BOOTSTRAP_SNIFF_BYTES must be at least 1",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv(testCase.envKey, testCase.envVal)

			_, err := Load()
			if err == nil {
				t.Fatalf("expected error")
			}
			if err.Error() != testCase.wantErr {
				t.Fatalf("expected error %q, got %q", testCase.wantErr, err.Error())
			}
		})
	}
}

func TestLoad_OutboundTimeout(t *testing.T) {
	setRequiredConfigEnv(t)

	t.Setenv("SHIVA_OUTBOUND_TIMEOUT_SECONDS", "42")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if cfg.OutboundTimeout != 42*time.Second {
		t.Fatalf("expected OutboundTimeout=42s, got %s", cfg.OutboundTimeout)
	}
}

func TestLoad_IngressAndTracingConfig(t *testing.T) {
	setRequiredConfigEnv(t)

	t.Setenv("SHIVA_INGRESS_BODY_LIMIT_BYTES", "2097152")
	t.Setenv("SHIVA_INGRESS_RATE_LIMIT_MAX", "20")
	t.Setenv("SHIVA_INGRESS_RATE_LIMIT_WINDOW_SECONDS", "30")
	t.Setenv("SHIVA_METRICS_PATH", "/metrics")
	t.Setenv("SHIVA_TRACING_ENABLED", "false")
	t.Setenv("SHIVA_TRACING_STDOUT", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if cfg.IngressBodyLimit != 2097152 {
		t.Fatalf("expected IngressBodyLimit=2097152, got %d", cfg.IngressBodyLimit)
	}
	if cfg.IngressRateLimitMax != 20 {
		t.Fatalf("expected IngressRateLimitMax=20, got %d", cfg.IngressRateLimitMax)
	}
	if cfg.IngressRateLimit != 30*time.Second {
		t.Fatalf("expected IngressRateLimit=30s, got %s", cfg.IngressRateLimit)
	}
	if cfg.MetricsPath != "/metrics" {
		t.Fatalf("expected MetricsPath=/metrics, got %q", cfg.MetricsPath)
	}
	if cfg.TracingEnabled {
		t.Fatalf("expected TracingEnabled=false")
	}
	if !cfg.TracingStdout {
		t.Fatalf("expected TracingStdout=true")
	}
}

func setRequiredConfigEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SHIVA_DATABASE_URL", "postgres://localhost:5432/shiva")
}
