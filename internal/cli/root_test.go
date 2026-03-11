package cli

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"github.com/iw2rmb/shiva/internal/cli/request"
)

func TestRootCommandDispatchesShorthandForms(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		args            []string
		expectedStdout  string
		expectedSpec    int
		expectedOp      int
		expectedCall    int
		expectedFormat  SpecFormat
		expectedRequest request.Envelope
	}{
		{
			name:           "repo selector",
			args:           []string{"allure/allure-deployment"},
			expectedStdout: "openapi: 3.1.0\n",
			expectedSpec:   1,
			expectedFormat: SpecFormatYAML,
			expectedRequest: request.Envelope{
				Kind: request.KindSpec,
				Repo: "allure/allure-deployment",
			},
		},
		{
			name:           "operation selector by id",
			args:           []string{"allure/allure-deployment#findAll_42"},
			expectedStdout: "{\"operationId\":\"findAll_42\"}\n",
			expectedOp:     1,
			expectedRequest: request.Envelope{
				Kind:        request.KindOperation,
				Repo:        "allure/allure-deployment",
				OperationID: "findAll_42",
			},
		},
		{
			name:           "operation selector by method path with yaml output",
			args:           []string{"-o", "yaml", "allure/allure-deployment", "PATCH", "/pets/:id"},
			expectedStdout: "operationId: patchPet\n",
			expectedOp:     1,
			expectedRequest: request.Envelope{
				Kind:   request.KindOperation,
				Repo:   "allure/allure-deployment",
				Method: "patch",
				Path:   "/pets/{id}",
			},
		},
		{
			name:           "call selector by target",
			args:           []string{"--dry-run", "allure/allure-deployment@shiva#getUsers"},
			expectedStdout: "{\"kind\":\"call\"}\n",
			expectedCall:   1,
			expectedFormat: SpecFormatJSON,
			expectedRequest: request.Envelope{
				Kind:        request.KindCall,
				Repo:        "allure/allure-deployment",
				Target:      "shiva",
				OperationID: "getUsers",
				DryRun:      true,
			},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			service := &fakeService{
				specBody:      []byte("openapi: 3.1.0\n"),
				operationBody: []byte(`{"operationId":"findAll_42"}`),
				callBody:      []byte(`{"kind":"call"}`),
			}
			if testCase.expectedRequest.Method == "patch" {
				service.operationBody = []byte(`{"operationId":"patchPet"}`)
			}

			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}
			command := NewRootCommand(func() (Service, error) {
				return service, nil
			})
			command.SetOut(stdout)
			command.SetErr(stderr)
			command.SetArgs(testCase.args)

			if err := command.ExecuteContext(context.Background()); err != nil {
				t.Fatalf("execute command failed: %v", err)
			}
			if stdout.String() != testCase.expectedStdout {
				t.Fatalf("expected stdout %q, got %q", testCase.expectedStdout, stdout.String())
			}
			if service.specCalls != testCase.expectedSpec {
				t.Fatalf("expected spec calls %d, got %d", testCase.expectedSpec, service.specCalls)
			}
			if service.operationCalls != testCase.expectedOp {
				t.Fatalf("expected operation calls %d, got %d", testCase.expectedOp, service.operationCalls)
			}
			if service.callCalls != testCase.expectedCall {
				t.Fatalf("expected call-plan calls %d, got %d", testCase.expectedCall, service.callCalls)
			}
			if testCase.expectedFormat != "" && service.lastFormat != testCase.expectedFormat {
				t.Fatalf("expected format %q, got %q", testCase.expectedFormat, service.lastFormat)
			}
			if !reflect.DeepEqual(service.lastRequest, testCase.expectedRequest) {
				t.Fatalf("expected request %+v, got %+v", testCase.expectedRequest, service.lastRequest)
			}
			if stderr.Len() != 0 {
				t.Fatalf("expected empty stderr, got %q", stderr.String())
			}
		})
	}
}

func TestRootCommandCompletionDoesNotLoadService(t *testing.T) {
	t.Parallel()

	loadCalls := 0
	stdout := &bytes.Buffer{}
	command := NewRootCommand(func() (Service, error) {
		loadCalls++
		return &fakeService{}, nil
	})
	command.SetOut(stdout)
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"completion", "bash"})

	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute completion command failed: %v", err)
	}
	if loadCalls != 0 {
		t.Fatalf("expected completion command to avoid loading service, got %d load calls", loadCalls)
	}
	if stdout.Len() == 0 {
		t.Fatalf("expected completion command to emit a script")
	}
}

func TestRootCommandHealthUsesService(t *testing.T) {
	t.Parallel()

	service := &fakeService{
		healthBody: []byte(`{"status":"ok"}`),
	}

	stdout := &bytes.Buffer{}
	command := NewRootCommand(func() (Service, error) {
		return service, nil
	})
	command.SetOut(stdout)
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"health"})

	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute health command failed: %v", err)
	}
	if service.healthCalls != 1 {
		t.Fatalf("expected one health call, got %d", service.healthCalls)
	}
	if stdout.String() != "{\"status\":\"ok\"}\n" {
		t.Fatalf("unexpected health stdout %q", stdout.String())
	}
}

type fakeService struct {
	specBody       []byte
	operationBody  []byte
	callBody       []byte
	healthBody     []byte
	specCalls      int
	operationCalls int
	callCalls      int
	healthCalls    int
	lastRequest    request.Envelope
	lastFormat     SpecFormat
	lastOptions    RequestOptions
}

func (s *fakeService) GetSpec(ctx context.Context, selector request.Envelope, options RequestOptions, format SpecFormat) ([]byte, error) {
	s.specCalls++
	s.lastRequest = selector
	s.lastFormat = format
	s.lastOptions = options
	return s.specBody, nil
}

func (s *fakeService) GetOperation(ctx context.Context, selector request.Envelope, options RequestOptions) ([]byte, error) {
	s.operationCalls++
	s.lastRequest = selector
	s.lastOptions = options
	return s.operationBody, nil
}

func (s *fakeService) PlanCall(ctx context.Context, selector request.Envelope, options RequestOptions) ([]byte, error) {
	s.callCalls++
	s.lastRequest = selector
	s.lastFormat = SpecFormatJSON
	s.lastOptions = options
	return s.callBody, nil
}

func (s *fakeService) Health(ctx context.Context, options RequestOptions) ([]byte, error) {
	s.healthCalls++
	s.lastOptions = options
	return s.healthBody, nil
}
