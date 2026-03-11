package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/iw2rmb/shiva/internal/cli"
)

func TestRunReturnsInvalidInputExitCodeForInvalidFallbackEnv(t *testing.T) {
	configHome := filepath.Join(t.TempDir(), "config")
	cacheHome := filepath.Join(t.TempDir(), "cache")

	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	t.Setenv("SHIVA_REQUEST_TIMEOUT_SECONDS", "0")

	originalArgs := os.Args
	t.Cleanup(func() {
		os.Args = originalArgs
	})
	os.Args = []string{"shiva", "health"}

	stdout, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatalf("create stdout temp file: %v", err)
	}
	defer stdout.Close()

	stderr, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatalf("create stderr temp file: %v", err)
	}
	defer stderr.Close()

	code := run(context.Background(), stdout, stderr)
	if code != cli.ExitCodeInvalidInput {
		t.Fatalf("expected invalid input exit code %d, got %d", cli.ExitCodeInvalidInput, code)
	}
}
