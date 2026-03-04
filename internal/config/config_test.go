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
			"SHIVA_GITLAB_WEBHOOK_SECRET",
			"SHIVA_TENANT_KEY",
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
