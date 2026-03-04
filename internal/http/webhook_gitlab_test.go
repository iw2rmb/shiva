package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iw2rmb/shiva/internal/config"
	"github.com/iw2rmb/shiva/internal/store"
)

const testGitLabPayload = `{
  "object_kind":"push",
  "ref":"refs/heads/main",
  "before":"1111111111111111111111111111111111111111",
  "after":"2222222222222222222222222222222222222222",
  "project":{"id":42,"path_with_namespace":"acme/platform-api","default_branch":"main"}
}`

type fakeGitLabIngestor struct {
	calls []store.GitLabIngestInput
	fn    func(context.Context, store.GitLabIngestInput) (store.GitLabIngestResult, error)
}

func (f *fakeGitLabIngestor) PersistGitLabWebhook(ctx context.Context, input store.GitLabIngestInput) (store.GitLabIngestResult, error) {
	f.calls = append(f.calls, input)
	if f.fn != nil {
		return f.fn(ctx, input)
	}
	return store.GitLabIngestResult{EventID: 1, Duplicate: false}, nil
}

func TestGitLabWebhookTokenVerification(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		tokenHeader   string
		expectedCode  int
		expectedCalls int
	}{
		{
			name:          "missing token returns unauthorized",
			tokenHeader:   "",
			expectedCode:  http.StatusUnauthorized,
			expectedCalls: 0,
		},
		{
			name:          "invalid token returns forbidden",
			tokenHeader:   "wrong",
			expectedCode:  http.StatusForbidden,
			expectedCalls: 0,
		},
		{
			name:          "valid token persists event",
			tokenHeader:   "secret-token",
			expectedCode:  http.StatusAccepted,
			expectedCalls: 1,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ingestor := &fakeGitLabIngestor{}
			server := newWebhookTestServer(ingestor)

			req := httptest.NewRequest(http.MethodPost, "/internal/webhooks/gitlab", strings.NewReader(testGitLabPayload))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Gitlab-Delivery", "delivery-1")
			if tc.tokenHeader != "" {
				req.Header.Set("X-Gitlab-Token", tc.tokenHeader)
			}

			resp, err := server.App().Test(req, -1)
			if err != nil {
				t.Fatalf("http test request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tc.expectedCode {
				t.Fatalf("expected status %d, got %d", tc.expectedCode, resp.StatusCode)
			}
			if len(ingestor.calls) != tc.expectedCalls {
				t.Fatalf("expected %d ingestor calls, got %d", tc.expectedCalls, len(ingestor.calls))
			}
		})
	}
}

func TestGitLabWebhookDuplicateDeliveryIdempotency(t *testing.T) {
	t.Parallel()

	seen := map[string]int64{}
	ingestor := &fakeGitLabIngestor{
		fn: func(_ context.Context, input store.GitLabIngestInput) (store.GitLabIngestResult, error) {
			if eventID, ok := seen[input.DeliveryID]; ok {
				return store.GitLabIngestResult{EventID: eventID, Duplicate: true}, nil
			}
			seen[input.DeliveryID] = 7001
			return store.GitLabIngestResult{EventID: 7001, Duplicate: false}, nil
		},
	}
	server := newWebhookTestServer(ingestor)

	first := doWebhookRequest(t, server, "delivery-7")
	second := doWebhookRequest(t, server, "delivery-7")

	if first.StatusCode != http.StatusAccepted {
		t.Fatalf("expected first status 202, got %d", first.StatusCode)
	}
	if second.StatusCode != http.StatusOK {
		t.Fatalf("expected duplicate status 200, got %d", second.StatusCode)
	}

	firstBody := decodeWebhookBody(t, first)
	secondBody := decodeWebhookBody(t, second)

	if firstBody.Duplicate {
		t.Fatalf("first response must not be duplicate")
	}
	if !secondBody.Duplicate {
		t.Fatalf("second response must be duplicate")
	}
	if firstBody.EventID != secondBody.EventID {
		t.Fatalf("expected same event id for duplicate delivery, got %d and %d", firstBody.EventID, secondBody.EventID)
	}
}

func newWebhookTestServer(ingestor gitlabWebhookIngestor) *Server {
	cfg := config.Config{
		HTTPAddr:            ":8080",
		GitLabWebhookSecret: "secret-token",
		TenantKey:           "tenant-a",
	}
	return newWebhookTestServerWithConfig(ingestor, cfg)
}

func newWebhookTestServerWithConfig(ingestor gitlabWebhookIngestor, cfg config.Config) *Server {
	server := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), &store.Store{})
	server.gitlabIngestor = ingestor
	return server
}

func doWebhookRequest(t *testing.T, server *Server, deliveryID string) *http.Response {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/internal/webhooks/gitlab", strings.NewReader(testGitLabPayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gitlab-Token", "secret-token")
	req.Header.Set("X-Gitlab-Delivery", deliveryID)

	resp, err := server.App().Test(req, -1)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	return resp
}

func decodeWebhookBody(t *testing.T, response *http.Response) gitlabWebhookResponse {
	t.Helper()
	defer response.Body.Close()

	var body gitlabWebhookResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	return body
}

func TestGitLabWebhookIngressBodyLimit(t *testing.T) {
	t.Parallel()

	ingestor := &fakeGitLabIngestor{}
	server := newWebhookTestServerWithConfig(ingestor, config.Config{
		HTTPAddr:            ":8080",
		GitLabWebhookSecret: "secret-token",
		TenantKey:           "tenant-a",
		IngressBodyLimit:    32,
	})

	oversizedBody := bytes.Repeat([]byte("x"), 128)
	req := httptest.NewRequest(http.MethodPost, "/internal/webhooks/gitlab", bytes.NewReader(oversizedBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gitlab-Token", "secret-token")
	req.Header.Set("X-Gitlab-Delivery", "delivery-oversized")

	resp, err := server.App().Test(req, -1)
	if err == nil {
		defer resp.Body.Close()
		t.Fatalf("expected request to fail due to body size limit")
	}
	if !strings.Contains(err.Error(), "body size exceeds the given limit") {
		t.Fatalf("expected body size limit error, got %v", err)
	}
	if len(ingestor.calls) != 0 {
		t.Fatalf("expected no ingestor calls for oversized request, got %d", len(ingestor.calls))
	}
}

func TestGitLabWebhookIngressRateLimit(t *testing.T) {
	t.Parallel()

	ingestor := &fakeGitLabIngestor{}
	server := newWebhookTestServerWithConfig(ingestor, config.Config{
		HTTPAddr:            ":8080",
		GitLabWebhookSecret: "secret-token",
		TenantKey:           "tenant-a",
		IngressRateLimitMax: 1,
		IngressRateLimit:    time.Minute,
		IngressBodyLimit:    1024 * 1024,
	})

	first := doWebhookRequest(t, server, "delivery-rate-1")
	defer first.Body.Close()
	second := doWebhookRequest(t, server, "delivery-rate-2")
	defer second.Body.Close()

	if first.StatusCode != http.StatusAccepted {
		t.Fatalf("expected first status 202, got %d", first.StatusCode)
	}
	if second.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected second status 429, got %d", second.StatusCode)
	}
	if len(ingestor.calls) != 1 {
		t.Fatalf("expected one ingestor call under rate limit, got %d", len(ingestor.calls))
	}
}
