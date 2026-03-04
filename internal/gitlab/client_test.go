package gitlab

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestClientCompareChangedPaths(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected method %s, got %s", http.MethodGet, r.Method)
		}
		if r.URL.Path != "/api/v4/projects/42/repository/compare" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("from"); got != "111111" {
			t.Fatalf("expected from=111111, got %q", got)
		}
		if got := r.URL.Query().Get("to"); got != "222222" {
			t.Fatalf("expected to=222222, got %q", got)
		}
		if got := r.Header.Get("PRIVATE-TOKEN"); got != "token-123" {
			t.Fatalf("expected private token header, got %q", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"diffs":[{"old_path":"openapi.yaml","new_path":"openapi.yaml","new_file":false,"renamed_file":false,"deleted_file":false},{"old_path":"specs/old.yaml","new_path":"specs/new.yaml","new_file":false,"renamed_file":true,"deleted_file":false}]}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token-123")
	if err != nil {
		t.Fatalf("NewClient() unexpected error: %v", err)
	}

	changed, err := client.CompareChangedPaths(context.Background(), 42, "111111", "222222")
	if err != nil {
		t.Fatalf("CompareChangedPaths() unexpected error: %v", err)
	}

	expected := []ChangedPath{
		{
			OldPath:     "openapi.yaml",
			NewPath:     "openapi.yaml",
			NewFile:     false,
			RenamedFile: false,
			DeletedFile: false,
		},
		{
			OldPath:     "specs/old.yaml",
			NewPath:     "specs/new.yaml",
			NewFile:     false,
			RenamedFile: true,
			DeletedFile: false,
		},
	}
	if !reflect.DeepEqual(changed, expected) {
		t.Fatalf("changed paths mismatch: expected %#v, got %#v", expected, changed)
	}
}

func TestClientCompareChangedPathsErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		statusCode  int
		response    string
		expectIs404 bool
	}{
		{
			name:        "not found maps to ErrNotFound",
			statusCode:  http.StatusNotFound,
			response:    `{"message":"404 Project Not Found"}`,
			expectIs404: true,
		},
		{
			name:        "server error returns APIError",
			statusCode:  http.StatusInternalServerError,
			response:    `boom`,
			expectIs404: false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.response))
			}))
			defer server.Close()

			client, err := NewClient(server.URL, "")
			if err != nil {
				t.Fatalf("NewClient() unexpected error: %v", err)
			}

			_, err = client.CompareChangedPaths(context.Background(), 7, "from", "to")
			if err == nil {
				t.Fatalf("expected error")
			}

			if tc.expectIs404 {
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("expected ErrNotFound, got %v", err)
				}
				return
			}

			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("expected APIError, got %T", err)
			}
			if apiErr.StatusCode != tc.statusCode {
				t.Fatalf("expected status %d, got %d", tc.statusCode, apiErr.StatusCode)
			}
		})
	}
}

func TestClientGetFileContent(t *testing.T) {
	t.Parallel()

	expectedContent := "openapi: 3.1.0\ninfo:\n  title: API\n"
	encodedContent := base64.StdEncoding.EncodeToString([]byte(expectedContent))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected method %s, got %s", http.MethodGet, r.Method)
		}
		if !strings.HasPrefix(r.RequestURI, "/api/v4/projects/42/repository/files/specs%2Fopenapi.yaml?") {
			t.Fatalf("unexpected request URI: %s", r.RequestURI)
		}
		if got := r.URL.Query().Get("ref"); got != "0123456789" {
			t.Fatalf("expected ref=0123456789, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"encoding":"base64","content":"` + encodedContent + `"}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "")
	if err != nil {
		t.Fatalf("NewClient() unexpected error: %v", err)
	}

	content, err := client.GetFileContent(context.Background(), 42, "specs/openapi.yaml", "0123456789")
	if err != nil {
		t.Fatalf("GetFileContent() unexpected error: %v", err)
	}
	if string(content) != expectedContent {
		t.Fatalf("expected content %q, got %q", expectedContent, string(content))
	}
}

func TestClientGetFileContentErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		statusCode  int
		response    string
		expectIs404 bool
		expectText  string
	}{
		{
			name:        "not found maps to ErrNotFound",
			statusCode:  http.StatusNotFound,
			response:    `{"message":"404 File Not Found"}`,
			expectIs404: true,
		},
		{
			name:       "unsupported encoding fails",
			statusCode: http.StatusOK,
			response:   `{"encoding":"plain","content":"abc"}`,
			expectText: "unsupported repository file encoding",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.response))
			}))
			defer server.Close()

			client, err := NewClient(server.URL, "")
			if err != nil {
				t.Fatalf("NewClient() unexpected error: %v", err)
			}

			_, err = client.GetFileContent(context.Background(), 7, "specs/openapi.yaml", "abcdef")
			if err == nil {
				t.Fatalf("expected error")
			}

			if tc.expectIs404 {
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("expected ErrNotFound, got %v", err)
				}
				return
			}

			if !strings.Contains(err.Error(), tc.expectText) {
				t.Fatalf("expected error to contain %q, got %v", tc.expectText, err)
			}
		})
	}
}

func TestClientListRepositoryTree(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		path         string
		recursive    bool
		makeServer   func(t *testing.T) *httptest.Server
		wantEntries  []TreeEntry
		wantErr      bool
		wantNotFound bool
	}{
		{
			name:      "aggregates all pages and returns files only",
			path:      "/specs/api",
			recursive: true,
			makeServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodGet {
						t.Fatalf("expected method %s, got %s", http.MethodGet, r.Method)
					}
					if r.URL.Path != "/api/v4/projects/42/repository/tree" {
						t.Fatalf("unexpected path: %s", r.URL.Path)
					}
					if got := r.URL.Query().Get("page"); got == "" {
						t.Fatalf("expected page query param")
					}

					switch r.URL.Query().Get("page") {
					case "1":
						w.Header().Set("X-Next-Page", "2")
						w.Header().Set("Content-Type", "application/json")
						_, _ = w.Write([]byte(`[
							{"id":"111","name":"root.yaml","type":"file","path":"/specs/openapi.yaml"},
							{"id":"112","name":"ignore","type":"tree","path":"specs/ignore"}
						]`))
					case "2":
						w.Header().Set("Content-Type", "application/json")
						_, _ = w.Write([]byte(`[
							{"id":"113","name":"nested.yaml","type":"blob","path":"/specs/nested/openapi.json"},
							{"id":"114","name":"other","type":"tree","path":"specs/other"}
						]`))
					default:
						t.Fatalf("unexpected page: %s", r.URL.Query().Get("page"))
					}
				}))
			},
			wantEntries: []TreeEntry{
				{Path: "specs/openapi.yaml", Type: "file"},
				{Path: "specs/nested/openapi.json", Type: "file"},
			},
		},
		{
			name:      "passes path ref recursive query params",
			path:      "/specs/api",
			recursive: true,
			makeServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if got := r.URL.Query().Get("ref"); got != "feedface" {
						t.Fatalf("expected ref=feedface, got %q", got)
					}
					if got := r.URL.Query().Get("recursive"); got != "true" {
						t.Fatalf("expected recursive=true, got %q", got)
					}
					if got := r.URL.Query().Get("path"); got != "specs/api" {
						t.Fatalf("expected path=specs/api, got %q", got)
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`[{"id":"111","name":"openapi.json","type":"file","path":"specs/openapi.json"}]`))
				}))
			},
			wantEntries: []TreeEntry{
				{Path: "specs/openapi.json", Type: "file"},
			},
		},
		{
			name:         "maps 404 to ErrNotFound",
			path:         "specs",
			recursive:    false,
			wantNotFound: true,
			wantErr:      true,
			makeServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusNotFound)
					_, _ = w.Write([]byte(`{"message":"404 Not Found"}`))
				}))
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := tc.makeServer(t)
			defer server.Close()

			client, err := NewClient(server.URL, "")
			if err != nil {
				t.Fatalf("NewClient() unexpected error: %v", err)
			}

			entries, err := client.ListRepositoryTree(context.Background(), 42, "feedface", tc.path, tc.recursive)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				if tc.wantNotFound {
					if !errors.Is(err, ErrNotFound) {
						t.Fatalf("expected ErrNotFound, got %v", err)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("ListRepositoryTree() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(entries, tc.wantEntries) {
				t.Fatalf("entries mismatch: expected %#v, got %#v", tc.wantEntries, entries)
			}
		})
	}
}
