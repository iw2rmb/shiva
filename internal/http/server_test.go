package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"io"
	"log/slog"

	"github.com/iw2rmb/shiva/internal/config"
	"github.com/iw2rmb/shiva/internal/store"
)

func TestHealthz(t *testing.T) {
	t.Parallel()

	cfg := config.Config{HTTPAddr: ":8080"}
	s := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), &store.Store{})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	resp, err := s.App().Test(req, -1)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestServer_DraftReadSurfaceDoesNotExposeQueryEndpoints(t *testing.T) {
	t.Parallel()

	cfg := config.Config{HTTPAddr: ":8080"}
	s := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), &store.Store{})

	testCases := []struct {
		name string
		path string
	}{
		{name: "spec query endpoint", path: "/v1/spec"},
		{name: "operation query endpoint", path: "/v1/operation"},
		{name: "apis query endpoint", path: "/v1/apis"},
		{name: "operations query endpoint", path: "/v1/operations"},
		{name: "repos query endpoint", path: "/v1/repos"},
		{name: "catalog status endpoint", path: "/v1/catalog/status"},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, testCase.path, nil)
			resp, err := s.App().Test(req, -1)
			if err != nil {
				t.Fatalf("http test request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("expected status 404 for %s, got %d", testCase.path, resp.StatusCode)
			}
		})
	}
}
