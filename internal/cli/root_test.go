package cli

import (
	"bytes"
	"context"
	"testing"
)

func TestRootCommandDispatchesSelectors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name              string
		args              []string
		expectedStdout    string
		expectedRepoCalls int
		expectedOpCalls   int
		expectedLastRepo  string
		expectedLastOpID  string
	}{
		{
			name:              "repo selector",
			args:              []string{"allure/allure-deployment"},
			expectedStdout:    "openapi: 3.1.0\n",
			expectedRepoCalls: 1,
			expectedLastRepo:  "allure/allure-deployment",
		},
		{
			name:             "operation selector",
			args:             []string{"allure/allure-deployment#findAll_42"},
			expectedStdout:   "{\"paths\":{}}\n",
			expectedOpCalls:  1,
			expectedLastRepo: "allure/allure-deployment",
			expectedLastOpID: "findAll_42",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			service := &fakeService{
				repoBody:      []byte("openapi: 3.1.0\n"),
				operationBody: []byte("{\"paths\":{}}"),
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
			if service.repoCalls != testCase.expectedRepoCalls {
				t.Fatalf("expected repo calls %d, got %d", testCase.expectedRepoCalls, service.repoCalls)
			}
			if service.operationCalls != testCase.expectedOpCalls {
				t.Fatalf("expected operation calls %d, got %d", testCase.expectedOpCalls, service.operationCalls)
			}
			if service.lastRepoPath != testCase.expectedLastRepo {
				t.Fatalf("expected last repo %q, got %q", testCase.expectedLastRepo, service.lastRepoPath)
			}
			if service.lastOperationID != testCase.expectedLastOpID {
				t.Fatalf("expected last operation id %q, got %q", testCase.expectedLastOpID, service.lastOperationID)
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

type fakeService struct {
	repoBody        []byte
	operationBody   []byte
	repoCalls       int
	operationCalls  int
	lastRepoPath    string
	lastOperationID string
}

func (s *fakeService) GetRepoSpec(ctx context.Context, repoPath string) ([]byte, error) {
	s.repoCalls++
	s.lastRepoPath = repoPath
	return s.repoBody, nil
}

func (s *fakeService) GetOperation(ctx context.Context, repoPath string, operationID string) ([]byte, error) {
	s.operationCalls++
	s.lastRepoPath = repoPath
	s.lastOperationID = operationID
	return s.operationBody, nil
}
