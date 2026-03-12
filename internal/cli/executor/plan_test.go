package executor

import (
	"bytes"
	"reflect"
	"testing"
	"time"

	"github.com/iw2rmb/shiva/internal/cli/request"
)

func TestPlanShivaCall(t *testing.T) {
	t.Parallel()

	actual, err := PlanShivaDispatchCall(request.Envelope{
		Kind:        request.KindCall,
		Namespace:   "acme",
		Repo:        "platform",
		API:         "apis/pets/openapi.yaml",
		RevisionID:  42,
		SHA:         "deadbeef",
		OperationID: "listPets",
		Method:      "get",
		Path:        "/pets",
		DryRun:      true,
	}, "https://shiva.example", "token", 5*time.Second)
	if err != nil {
		t.Fatalf("plan shiva call failed: %v", err)
	}

	expected := CallPlan{
		Request: request.Envelope{
			Kind:        request.KindCall,
			Namespace:   "acme",
			Repo:        "platform",
			API:         "apis/pets/openapi.yaml",
			RevisionID:  42,
			SHA:         "deadbeef",
			Target:      request.DefaultShivaTarget,
			OperationID: "listPets",
			Method:      "get",
			Path:        "/pets",
			DryRun:      true,
		},
		Dispatch: DispatchPlan{
			Mode:    DispatchModeShiva,
			DryRun:  true,
			Network: false,
			Request: HTTPRequest{
				Method: "POST",
				URL:    "https://shiva.example/v1/call",
				Headers: map[string][]string{
					"Authorization": []string{"Bearer token"},
					"Content-Type":  []string{"application/json"},
				},
				Body: []byte(`{"kind":"call","namespace":"acme","repo":"platform","api":"apis/pets/openapi.yaml","revision_id":42,"sha":"deadbeef","target":"shiva","operation_id":"listPets","method":"get","path":"/pets","dry_run":true}`),
			},
			Timeout: 5 * time.Second,
		},
	}
	if !reflect.DeepEqual(actual.Request, expected.Request) {
		t.Fatalf("expected request %+v, got %+v", expected.Request, actual.Request)
	}
	if actual.Dispatch.Mode != expected.Dispatch.Mode ||
		actual.Dispatch.DryRun != expected.Dispatch.DryRun ||
		actual.Dispatch.Network != expected.Dispatch.Network ||
		actual.Dispatch.Timeout != expected.Dispatch.Timeout ||
		!reflect.DeepEqual(actual.Dispatch.Request.Headers, expected.Dispatch.Request.Headers) ||
		!bytes.Equal(actual.Dispatch.Request.Body, expected.Dispatch.Request.Body) ||
		actual.Dispatch.Request.Method != expected.Dispatch.Request.Method ||
		actual.Dispatch.Request.URL != expected.Dispatch.Request.URL {
		t.Fatalf("expected dispatch %+v, got %+v", expected.Dispatch, actual.Dispatch)
	}
}

func TestPlanDirectCall(t *testing.T) {
	t.Parallel()

	actual, err := PlanDirectCall(request.Envelope{
		Kind:        request.KindCall,
		Namespace:   "acme",
		Repo:        "platform",
		API:         "apis/pets/openapi.yaml",
		RevisionID:  42,
		SHA:         "deadbeef",
		Target:      "prod",
		OperationID: "getPet",
		Method:      "get",
		Path:        "/pets/{id}",
		PathParams:  map[string]string{"id": "42"},
		QueryParams: map[string][]string{"expand": []string{"owners", "metrics"}},
		Headers:     map[string][]string{"X-Trace": []string{"abc"}},
	}, "https://api.example", "token", 5*time.Second)
	if err != nil {
		t.Fatalf("plan direct call failed: %v", err)
	}

	expected := CallPlan{
		Request: request.Envelope{
			Kind:        request.KindCall,
			Namespace:   "acme",
			Repo:        "platform",
			API:         "apis/pets/openapi.yaml",
			RevisionID:  42,
			SHA:         "deadbeef",
			Target:      "prod",
			OperationID: "getPet",
			Method:      "get",
			Path:        "/pets/{id}",
			PathParams:  map[string]string{"id": "42"},
			QueryParams: map[string][]string{"expand": []string{"owners", "metrics"}},
			Headers:     map[string][]string{"X-Trace": []string{"abc"}},
		},
		Dispatch: DispatchPlan{
			Mode:    DispatchModeDirect,
			DryRun:  false,
			Network: true,
			Request: HTTPRequest{
				Method: "GET",
				URL:    "https://api.example/pets/42?expand=owners&expand=metrics",
				Headers: map[string][]string{
					"Authorization": []string{"Bearer token"},
					"X-Trace":       []string{"abc"},
				},
			},
			Timeout: 5 * time.Second,
		},
	}
	if !reflect.DeepEqual(actual.Request, expected.Request) {
		t.Fatalf("expected request %+v, got %+v", expected.Request, actual.Request)
	}
	if actual.Dispatch.Mode != expected.Dispatch.Mode ||
		actual.Dispatch.DryRun != expected.Dispatch.DryRun ||
		actual.Dispatch.Network != expected.Dispatch.Network ||
		actual.Dispatch.Timeout != expected.Dispatch.Timeout ||
		!reflect.DeepEqual(actual.Dispatch.Request.Headers, expected.Dispatch.Request.Headers) ||
		!bytes.Equal(actual.Dispatch.Request.Body, expected.Dispatch.Request.Body) ||
		actual.Dispatch.Request.Method != expected.Dispatch.Request.Method ||
		actual.Dispatch.Request.URL != expected.Dispatch.Request.URL {
		t.Fatalf("expected dispatch %+v, got %+v", expected.Dispatch, actual.Dispatch)
	}
}

func TestPlanDirectCallRejectsMissingOrUnusedPathParams(t *testing.T) {
	t.Parallel()

	_, err := PlanDirectCall(request.Envelope{
		Kind:        request.KindCall,
		Namespace:   "acme",
		Repo:        "platform",
		API:         "apis/pets/openapi.yaml",
		RevisionID:  42,
		Target:      "prod",
		OperationID: "getPet",
		Method:      "get",
		Path:        "/pets/{id}",
		PathParams:  map[string]string{"other": "42"},
	}, "https://api.example", "", 5*time.Second)
	if err == nil {
		t.Fatal("expected path parameter validation error")
	}
	if err.Error() != `path parameter "id" is required` {
		t.Fatalf("unexpected error %q", err.Error())
	}
}
