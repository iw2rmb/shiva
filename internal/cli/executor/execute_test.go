package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/iw2rmb/shiva/internal/cli/request"
)

func TestExecute(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/pets" {
			t.Fatalf("expected /pets, got %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if string(body) != `{"name":"Milo"}` {
			t.Fatalf("unexpected request body %q", string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"42"}`))
	}))
	defer server.Close()

	plan, err := PlanDirectCall(request.Envelope{
		Kind:       request.KindCall,
		Namespace:  "acme",
		Repo:       "platform",
		API:        "apis/pets/openapi.yaml",
		RevisionID: 42,
		Target:     "prod",
		Method:     "post",
		Path:       "/pets",
		JSONBody:   []byte(`{"name":"Milo"}`),
	}, server.URL, "", 5*time.Second)
	if err != nil {
		t.Fatalf("plan direct call failed: %v", err)
	}

	response, err := Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, response.StatusCode)
	}
	if string(response.Body) != `{"id":"42"}` {
		t.Fatalf("unexpected response body %q", string(response.Body))
	}
}

func TestPlanDirectCallPreservesRawBody(t *testing.T) {
	t.Parallel()

	plan, err := PlanDirectCall(request.Envelope{
		Kind:       request.KindCall,
		Namespace:  "acme",
		Repo:       "platform",
		API:        "apis/pets/openapi.yaml",
		RevisionID: 42,
		Target:     "prod",
		Method:     "post",
		Path:       "/pets",
		Body:       "  raw body\n",
	}, "https://api.example", "", 5*time.Second)
	if err != nil {
		t.Fatalf("plan direct call failed: %v", err)
	}
	if string(plan.Dispatch.Request.Body) != "  raw body\n" {
		t.Fatalf("expected raw body to be preserved, got %q", string(plan.Dispatch.Request.Body))
	}
}
