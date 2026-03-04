package config

import (
	"os"
	"testing"
	"time"
)

func TestLoad_DefaultValues(t *testing.T) {
	t.Cleanup(func() {
		for _, name := range []string{
			"SHIVA_HTTP_ADDR",
			"SHIVA_LOG_LEVEL",
			"SHIVA_WORKER_CONCURRENCY",
			"SHIVA_SHUTDOWN_TIMEOUT_SECONDS",
			"SHIVA_GITLAB_BASE_URL",
			"SHIVA_GITLAB_TOKEN",
			"SHIVA_GITLAB_WEBHOOK_SECRET",
			"SHIVA_TENANT_KEY",
			"SHIVA_OPENAPI_PATH_GLOBS",
			"SHIVA_OPENAPI_REF_MAX_FETCHES",
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
	if cfg.TenantKey != "default" {
		t.Fatalf("expected default tenant key \"default\", got %q", cfg.TenantKey)
	}
	if len(cfg.OpenAPIPathGlobs) == 0 {
		t.Fatalf("expected default openapi path globs to be configured")
	}
	if cfg.OpenAPIRefMaxFetches != 128 {
		t.Fatalf("expected default openapi ref max fetches 128, got %d", cfg.OpenAPIRefMaxFetches)
	}
}

func TestLoad_InvalidWorkerConcurrency(t *testing.T) {
	t.Setenv("SHIVA_WORKER_CONCURRENCY", "zero")
	_, err := Load()
	if err == nil {
		t.Fatalf("expected error for invalid worker concurrency")
	}
}

func TestLoad_RejectsEmptyTenantKey(t *testing.T) {
	t.Setenv("SHIVA_TENANT_KEY", "  ")
	_, err := Load()
	if err == nil {
		t.Fatalf("expected error for empty tenant key")
	}
}

func TestLoad_OpenAPIConfig(t *testing.T) {
	t.Setenv("SHIVA_OPENAPI_PATH_GLOBS", "specs/**/*.yaml,docs/swagger*.yml")
	t.Setenv("SHIVA_OPENAPI_REF_MAX_FETCHES", "64")

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
}
