package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDocumentFallsBackToDefaultProfileWhenConfigMissing(t *testing.T) {
	t.Parallel()

	document, err := LoadDocument(LoadOptions{
		ConfigHome: t.TempDir(),
		Overrides: EnvOverrides{
			BaseURL: "http://127.0.0.1:9090",
			Timeout: 15 * time.Second,
		},
	})
	if err != nil {
		t.Fatalf("load document failed: %v", err)
	}

	if document.ActiveProfile != "default" {
		t.Fatalf("expected active profile default, got %q", document.ActiveProfile)
	}
	if document.Profiles["default"].BaseURL != "http://127.0.0.1:9090" {
		t.Fatalf("unexpected default profile base url %q", document.Profiles["default"].BaseURL)
	}
	if document.Profiles["default"].Timeout != 15*time.Second {
		t.Fatalf("unexpected default profile timeout %s", document.Profiles["default"].Timeout)
	}
	if _, ok := document.Targets["shiva"]; !ok {
		t.Fatalf("expected built-in shiva target to exist")
	}
}

func TestDocumentResolveSourcePrefersExplicitProfileOverTargetOverride(t *testing.T) {
	t.Parallel()

	configHome := t.TempDir()
	configPath := filepath.Join(configHome, "shiva", "profiles.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(`
active_profile: default
profiles:
  default:
    base_url: http://default.example
    timeout: 10s
  prod-source:
    base_url: http://prod-source.example
    timeout: 10s
targets:
  prod:
    mode: direct
    base_url: https://api.example
    timeout: 10s
    source_profile: prod-source
`), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	document, err := LoadDocument(LoadOptions{ConfigHome: configHome})
	if err != nil {
		t.Fatalf("load document failed: %v", err)
	}

	resolvedByTarget, _, err := document.ResolveSource("", "prod")
	if err != nil {
		t.Fatalf("resolve target override failed: %v", err)
	}
	if resolvedByTarget.Name != "prod-source" {
		t.Fatalf("expected target override to use prod-source, got %q", resolvedByTarget.Name)
	}

	resolvedExplicit, _, err := document.ResolveSource("default", "prod")
	if err != nil {
		t.Fatalf("resolve explicit profile failed: %v", err)
	}
	if resolvedExplicit.Name != "default" {
		t.Fatalf("expected explicit profile to win, got %q", resolvedExplicit.Name)
	}
}

func TestResolvePathsUsesXDGEnvironment(t *testing.T) {
	t.Setenv("HOME", "/tmp/home")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-config")
	t.Setenv("XDG_CACHE_HOME", "/tmp/xdg-cache")

	paths, err := ResolvePaths()
	if err != nil {
		t.Fatalf("resolve paths failed: %v", err)
	}

	if paths.ConfigHome != "/tmp/xdg-config" {
		t.Fatalf("unexpected config home %q", paths.ConfigHome)
	}
	if paths.CacheHome != "/tmp/xdg-cache" {
		t.Fatalf("unexpected cache home %q", paths.CacheHome)
	}
}

func TestLoadDocumentIgnoresFallbackEnvWhenConfigExists(t *testing.T) {
	configHome := t.TempDir()
	configPath := filepath.Join(configHome, "shiva", "profiles.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(`
active_profile: default
profiles:
  default:
    base_url: http://127.0.0.1:8080
    timeout: 10s
`), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	t.Setenv("SHIVA_REQUEST_TIMEOUT_SECONDS", "0")

	document, err := LoadDocument(LoadOptions{ConfigHome: configHome})
	if err != nil {
		t.Fatalf("load document failed: %v", err)
	}
	if document.ActiveProfile != "default" {
		t.Fatalf("unexpected active profile %q", document.ActiveProfile)
	}
}
