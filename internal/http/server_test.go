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

func TestServer_QueryReadSurfaceIsRegistered(t *testing.T) {
	t.Parallel()

	cfg := config.Config{HTTPAddr: ":8080"}
	s := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), &store.Store{})

	testCases := []struct {
		name string
		path string
	}{
		{name: "spec query endpoint", path: "/v1/spec?repo=acme%2Fplatform"},
		{name: "operation query endpoint", path: "/v1/operation?repo=acme%2Fplatform&operation_id=listPets"},
		{name: "apis query endpoint", path: "/v1/apis?repo=acme%2Fplatform"},
		{name: "operations query endpoint", path: "/v1/operations?repo=acme%2Fplatform"},
		{name: "repos query endpoint", path: "/v1/repos"},
		{name: "catalog status endpoint", path: "/v1/catalog/status?repo=acme%2Fplatform"},
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

			if resp.StatusCode == http.StatusNotFound {
				t.Fatalf("expected registered route for %s, got 404", testCase.path)
			}
		})
	}
}

func TestServer_LegacyReadSurfaceRemoved(t *testing.T) {
	t.Parallel()

	cfg := config.Config{HTTPAddr: ":8080"}
	s := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), &store.Store{})

	testCases := []string{
		"/v1/specs/acme%2Fplatform/openapi.json",
		"/v1/routes/acme%2Fplatform/%2Fpets",
	}

	for _, path := range testCases {
		resp, err := s.App().Test(httptest.NewRequest(http.MethodGet, path, nil), -1)
		if err != nil {
			t.Fatalf("http test request failed for %s: %v", path, err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected status 404 for legacy path %s, got %d", path, resp.StatusCode)
		}
	}
}
