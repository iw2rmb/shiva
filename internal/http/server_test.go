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
