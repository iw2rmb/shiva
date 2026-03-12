package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/iw2rmb/shiva/internal/cli/request"
)

func TestRootCommandAppliesCallInputFlags(t *testing.T) {
	t.Parallel()

	jsonPath := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(jsonPath, []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatalf("write json fixture: %v", err)
	}

	service := &fakeService{
		callBody: []byte(`{"status":"ok"}`),
	}

	stdout := &bytes.Buffer{}
	command := NewRootCommand(func() (Service, error) {
		return service, nil
	})
	command.SetOut(stdout)
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{
		"--via", "prod",
		"--path", "id=42",
		"--query", "expand=owners",
		"--query", "expand=metrics",
		"--header", "X-Trace=abc",
		"--json", "@" + jsonPath,
		"acme/platform#getPet",
	})

	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute command failed: %v", err)
	}
	if stdout.String() != "{\"status\":\"ok\"}\n" {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}

	expected := request.Envelope{
		Kind:        request.KindCall,
		Namespace:   "acme",
		Repo:        "platform",
		Target:      "prod",
		OperationID: "getPet",
		PathParams:  map[string]string{"id": "42"},
		QueryParams: map[string][]string{"expand": []string{"owners", "metrics"}},
		Headers:     map[string][]string{"X-Trace": []string{"abc"}},
		JSONBody:    []byte(`{"ok":true}`),
	}
	if !reflect.DeepEqual(service.lastRequest, expected) {
		t.Fatalf("expected request %+v, got %+v", expected, service.lastRequest)
	}
	if service.lastCallFormat != CallFormatBody {
		t.Fatalf("expected call format %q, got %q", CallFormatBody, service.lastCallFormat)
	}
}

func TestRootCommandListRejectsRemovedEmitFlag(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	command := NewRootCommand(func() (Service, error) {
		return &fakeService{}, nil
	})
	command.SetOut(stdout)
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"ls", "--emit", "request"})

	err := command.ExecuteContext(context.Background())
	if err == nil {
		t.Fatalf("expected ls to reject removed --emit flag")
	}
	if err.Error() != "unknown flag: --emit" {
		t.Fatalf("unexpected error %q", err.Error())
	}
}

func TestRootCommandBatchReadsNDJSONAndAppliesBatchDryRun(t *testing.T) {
	t.Parallel()

	service := &fakeService{
		callBody: []byte(`{"kind":"call"}`),
	}

	stdout := &bytes.Buffer{}
	command := NewRootCommand(func() (Service, error) {
		return service, nil
	})
	command.SetOut(stdout)
	command.SetErr(&bytes.Buffer{})
	command.SetIn(strings.NewReader(`{"kind":"call","namespace":"acme","repo":"platform","target":"prod","operation_id":"getPet"}` + "\n"))
	command.SetArgs([]string{"batch", "--dry-run"})

	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute batch command failed: %v", err)
	}
	if service.callCalls != 1 {
		t.Fatalf("expected one call execution, got %d", service.callCalls)
	}
	if !service.lastRequest.DryRun {
		t.Fatalf("expected batch --dry-run to mark the call request, got %+v", service.lastRequest)
	}
	if !strings.Contains(stdout.String(), `"ok":true`) || !strings.Contains(stdout.String(), `"dry_run":true`) {
		t.Fatalf("expected ndjson batch output, got %q", stdout.String())
	}
}
