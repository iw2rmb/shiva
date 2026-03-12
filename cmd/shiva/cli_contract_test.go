package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestCLIContractScenarios(t *testing.T) {
	server := newCLIContractServer(t)
	defer server.Close()

	testCases := []struct {
		name           string
		args           []string
		stdin          string
		wantCode       int
		wantStdout     []string
		wantStderr     string
		wantMinCalls   map[string]int
	}{
		{
			name:       "spec shorthand fetches pinned yaml spec",
			args:       []string{"acme/platform"},
			wantCode:   0,
			wantStdout: []string{"openapi: 3.1.0", "title: Pets API"},
			wantMinCalls: map[string]int{
				"GET /v1/repos":          1,
				"GET /v1/catalog/status": 1,
				"GET /v1/apis":           1,
				"GET /v1/spec":           1,
			},
		},
		{
			name:       "operation shorthand resolves by operation id",
			args:       []string{"acme/platform#getPet"},
			wantCode:   0,
			wantStdout: []string{`"operationId":"getPet"`, `"summary":"Get pet"`},
			wantMinCalls: map[string]int{
				"GET /v1/repos":          1,
				"GET /v1/catalog/status": 1,
				"GET /v1/apis":           1,
				"GET /v1/operations":     1,
				"GET /v1/operation":      1,
			},
		},
		{
			name:       "method path shorthand normalizes colon params",
			args:       []string{"acme/platform", "GET", "/pets/:id"},
			wantCode:   0,
			wantStdout: []string{`"operationId":"getPet"`},
			wantMinCalls: map[string]int{
				"GET /v1/repos":          1,
				"GET /v1/catalog/status": 1,
				"GET /v1/apis":           1,
				"GET /v1/operations":     1,
				"GET /v1/operation":      1,
			},
		},
		{
			name:       "direct dry run uses configured target",
			args:       []string{"--dry-run", "-o", "curl", "--path", "id=42", "--query", "expand=owner", "acme/platform@prod#getPet"},
			wantCode:   0,
			wantStdout: []string{"curl -X GET", "https://api.example.test/pets/42?expand=owner"},
			wantMinCalls: map[string]int{
				"GET /v1/repos":          1,
				"GET /v1/catalog/status": 1,
				"GET /v1/apis":           1,
				"GET /v1/operations":     1,
			},
		},
		{
			name:       "list repo shows operations from selector-driven ls",
			args:       []string{"ls", "acme/platform"},
			wantCode:   0,
			wantStdout: []string{"namespace acme, total 1 repos", "platform", "main (deadbeef), total 1 ops", "GET /pets/:id", "#getPet"},
			wantMinCalls: map[string]int{
				"GET /v1/repos":      1,
				"GET /v1/operations": 1,
			},
		},
		{
			name:       "sync refreshes api and operation catalogs",
			args:       []string{"sync", "acme/platform"},
			wantCode:   0,
			wantStdout: []string{`"namespace":"acme","repo":"platform"`, `"scope":"default-branch-latest"`, `"operation_catalog_count":2`},
			wantMinCalls: map[string]int{
				"GET /v1/repos":          1,
				"GET /v1/catalog/status": 1,
				"GET /v1/apis":           1,
				"GET /v1/operations":     2,
			},
		},
		{
			name: "batch runs mixed request envelopes",
			args: []string{"batch"},
			stdin: strings.Join([]string{
				`{"kind":"spec","namespace":"acme","repo":"platform","api":"apis/pets/openapi.yaml","revision_id":42}`,
				`{"kind":"operation","namespace":"acme","repo":"platform","operation_id":"getPet","revision_id":42}`,
				`{"kind":"call","namespace":"acme","repo":"platform","operation_id":"getPet"}`,
				"",
			}, "\n"),
			wantCode:   0,
			wantStdout: []string{`"index":0`, `"format":"json"`, `"mode":"shiva"`},
			wantMinCalls: map[string]int{
				"GET /v1/spec":       1,
				"GET /v1/operation":  1,
				"GET /v1/repos":      1,
				"GET /v1/catalog/status": 1,
				"GET /v1/apis":       1,
				"GET /v1/operations": 1,
				"POST /v1/call":      1,
			},
		},
	}

	for _, testCase := range testCases {
		server.Reset()
		env := writeCLIContractConfig(t, server.URL)

		code, stdout, stderr := runCLIContractCommand(t, env, testCase.args, testCase.stdin)
		if code != testCase.wantCode {
			t.Fatalf("%s: expected exit code %d, got %d (stderr=%q)", testCase.name, testCase.wantCode, code, stderr)
		}
		for _, want := range testCase.wantStdout {
			if !strings.Contains(stdout, want) {
				t.Fatalf("%s: expected stdout to contain %q, got %q", testCase.name, want, stdout)
			}
		}
		if stderr != testCase.wantStderr {
			t.Fatalf("%s: expected stderr %q, got %q", testCase.name, testCase.wantStderr, stderr)
		}
		for key, want := range testCase.wantMinCalls {
			if got := server.CallCount(key); got < want {
				t.Fatalf("%s: expected at least %d call(s) to %s, got %d", testCase.name, want, key, got)
			}
		}
	}
}

type cliContractEnv struct {
	home string
}

func writeCLIContractConfig(t *testing.T, baseURL string) cliContractEnv {
	t.Helper()

	home := t.TempDir()
	configDir := filepath.Join(home, ".config", "shiva")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}

	config := strings.TrimSpace(`
active_profile: default
profiles:
  default:
    base_url: ` + baseURL + `
    timeout: 2s
targets:
  prod:
    mode: direct
    source_profile: default
    base_url: https://api.example.test
    timeout: 2s
`)
	if err := os.WriteFile(filepath.Join(configDir, "profiles.yaml"), []byte(config+"\n"), 0o644); err != nil {
		t.Fatalf("write profiles config: %v", err)
	}

	return cliContractEnv{home: home}
}

func runCLIContractCommand(t *testing.T, env cliContractEnv, args []string, stdin string) (int, string, string) {
	t.Helper()

	originalArgs := os.Args
	originalStdin := os.Stdin
	t.Cleanup(func() {
		os.Args = originalArgs
		os.Stdin = originalStdin
	})

	stdout, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatalf("create stdout: %v", err)
	}
	defer stdout.Close()

	stderr, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatalf("create stderr: %v", err)
	}
	defer stderr.Close()

	if stdin != "" {
		stdinFile, err := os.CreateTemp(t.TempDir(), "stdin")
		if err != nil {
			t.Fatalf("create stdin: %v", err)
		}
		if _, err := stdinFile.WriteString(stdin); err != nil {
			t.Fatalf("seed stdin: %v", err)
		}
		if _, err := stdinFile.Seek(0, 0); err != nil {
			t.Fatalf("rewind stdin: %v", err)
		}
		defer stdinFile.Close()
		os.Stdin = stdinFile
	}

	t.Setenv("HOME", env.home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(env.home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(env.home, ".cache"))

	os.Args = append([]string{"shiva"}, args...)
	code := run(context.Background(), stdout, stderr)

	stdoutBody, err := os.ReadFile(stdout.Name())
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	stderrBody, err := os.ReadFile(stderr.Name())
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}

	return code, string(stdoutBody), string(stderrBody)
}

type cliContractServer struct {
	*httptest.Server
	mu     sync.Mutex
	counts map[string]int
}

func newCLIContractServer(t *testing.T) *cliContractServer {
	t.Helper()

	server := &cliContractServer{counts: make(map[string]int)}
	server.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.record(r.Method + " " + r.URL.Path)

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/healthz":
			writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/repos":
			writeJSON(w, http.StatusOK, []map[string]any{{
				"namespace":        "acme",
				"repo":             "platform",
				"default_branch":   "main",
				"active_api_count": 1,
				"snapshot_revision": map[string]any{
					"id":  42,
					"sha": "deadbeef",
				},
				"head_revision": map[string]any{
					"id":  43,
					"sha": "feedbeef",
				},
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/catalog/status":
			if r.URL.Query().Get("namespace") != "acme" || r.URL.Query().Get("repo") != "platform" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unexpected repo query"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"namespace": "acme",
				"repo":      "platform",
				"snapshot_revision": map[string]any{
					"id":  42,
					"sha": "deadbeef",
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/apis":
			writeJSON(w, http.StatusOK, []map[string]any{{
				"api":                  "apis/pets/openapi.yaml",
				"status":               "active",
				"display_name":         "Pets API",
				"has_snapshot":         true,
				"api_spec_revision_id": 7,
				"ingest_event_id":      42,
				"ingest_event_sha":     "deadbeef",
				"ingest_event_branch":  "main",
				"spec_etag":            "etag-1",
				"spec_size_bytes":      128,
				"operation_count":      1,
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/operations":
			writeJSON(w, http.StatusOK, []map[string]any{{
				"api":                  "apis/pets/openapi.yaml",
				"status":               "active",
				"api_spec_revision_id": 7,
				"ingest_event_id":      42,
				"ingest_event_sha":     "deadbeef",
				"ingest_event_branch":  "main",
				"method":               "get",
				"path":                 "/pets/{id}",
				"operation_id":         "getPet",
				"summary":              "Get pet",
				"deprecated":           false,
				"operation": map[string]any{
					"operationId": "getPet",
					"summary":     "Get pet",
				},
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/spec":
			assertPinnedRevision(t, r.URL.Query())
			switch r.URL.Query().Get("format") {
			case "yaml":
				w.Header().Set("Content-Type", "application/yaml")
				_, _ = w.Write([]byte("openapi: 3.1.0\ninfo:\n  title: Pets API\n"))
			case "json":
				writeJSON(w, http.StatusOK, map[string]any{"openapi": "3.1.0"})
			default:
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unexpected format"})
			}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/operation":
			assertPinnedRevision(t, r.URL.Query())
			query := r.URL.Query()
			switch {
			case query.Get("operation_id") == "getPet":
			case query.Get("method") == "get" && query.Get("path") == "/pets/{id}":
			default:
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unexpected operation selector"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"operationId": "getPet",
				"summary":     "Get pet",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/call":
			var envelope map[string]any
			if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
				t.Fatalf("decode call body: %v", err)
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"request": envelope,
				"dispatch": map[string]any{
					"mode":    "shiva",
					"dry_run": false,
					"network": false,
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	return server
}

func (s *cliContractServer) record(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counts[key]++
}

func (s *cliContractServer) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counts = make(map[string]int)
}

func (s *cliContractServer) CallCount(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[key]
}

func assertPinnedRevision(t *testing.T, query url.Values) {
	t.Helper()
	if query.Get("revision_id") != "42" {
		t.Fatalf("expected pinned revision_id=42, got query %v", query)
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
