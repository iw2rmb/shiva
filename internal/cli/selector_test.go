package cli

import (
	"reflect"
	"testing"

	"github.com/iw2rmb/shiva/internal/cli/request"
)

func TestParsePackedSelector(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		input       string
		expected    PackedSelector
		expectedErr string
	}{
		{
			name:  "repo only",
			input: "allure/allure-deployment",
			expected: PackedSelector{
				Namespace: "allure",
				Repo:      "allure-deployment",
			},
		},
		{
			name:  "repo and operation",
			input: "allure/allure-deployment#findAll_42",
			expected: PackedSelector{
				Namespace:   "allure",
				Repo:        "allure-deployment",
				OperationID: "findAll_42",
			},
		},
		{
			name:  "repo target and operation",
			input: "allure/allure-deployment@shiva#getUsers",
			expected: PackedSelector{
				Namespace:   "allure",
				Repo:        "allure-deployment",
				Target:      "shiva",
				OperationID: "getUsers",
			},
		},
		{
			name:        "empty selector",
			input:       "   ",
			expectedErr: "selector must not be empty",
		},
		{
			name:        "empty repo",
			input:       "#findAll_42",
			expectedErr: "repo path must not be empty",
		},
		{
			name:        "empty target",
			input:       "allure/allure-deployment@#findAll_42",
			expectedErr: "target must not be empty",
		},
		{
			name:        "empty operation",
			input:       "allure/allure-deployment#",
			expectedErr: "operation id must not be empty",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			actual, err := ParsePackedSelector(testCase.input)
			if testCase.expectedErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", testCase.expectedErr)
				}
				if err.Error() != testCase.expectedErr {
					t.Fatalf("expected error %q, got %q", testCase.expectedErr, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("parse selector failed: %v", err)
			}
			if actual != testCase.expected {
				t.Fatalf("expected selector %#v, got %#v", testCase.expected, actual)
			}
		})
	}
}

func TestParseShorthandInvocation(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		args        []string
		flags       RootFlags
		expected    ShorthandInvocation
		expectedErr string
	}{
		{
			name: "repo spec",
			args: []string{"allure/allure-deployment"},
			expected: ShorthandInvocation{
				Envelope: request.Envelope{
					Kind:      request.KindSpec,
					Namespace: "allure",
					Repo:      "allure-deployment",
				},
			},
		},
		{
			name: "operation lookup by id",
			args: []string{"allure/allure-deployment#findAll_42"},
			flags: RootFlags{
				API:        "service-catalog/allure-api.yaml",
				RevisionID: 146,
			},
			expected: ShorthandInvocation{
				Envelope: request.Envelope{
					Kind:        request.KindOperation,
					Namespace:   "allure",
					Repo:        "allure-deployment",
					API:         "service-catalog/allure-api.yaml",
					RevisionID:  146,
					OperationID: "findAll_42",
				},
			},
		},
		{
			name: "operation lookup by method path",
			args: []string{"allure/allure-deployment", "PATCH", "/accessgroup/:id/user"},
			expected: ShorthandInvocation{
				Envelope: request.Envelope{
					Kind:      request.KindOperation,
					Namespace: "allure",
					Repo:      "allure-deployment",
					Method:    "patch",
					Path:      "/accessgroup/{id}/user",
				},
			},
		},
		{
			name: "call lookup by packed target",
			args: []string{"allure/allure-deployment@shiva#getUsers"},
			flags: RootFlags{
				DryRun: true,
			},
			expected: ShorthandInvocation{
				Envelope: request.Envelope{
					Kind:        request.KindCall,
					Namespace:   "allure",
					Repo:        "allure-deployment",
					Target:      "shiva",
					OperationID: "getUsers",
					DryRun:      true,
				},
			},
		},
		{
			name: "call lookup by via flag",
			args: []string{"allure/allure-deployment", "get", "pets/:id"},
			flags: RootFlags{
				Target: "shiva",
			},
			expected: ShorthandInvocation{
				Envelope: request.Envelope{
					Kind:      request.KindCall,
					Namespace: "allure",
					Repo:      "allure-deployment",
					Target:    "shiva",
					Method:    "get",
					Path:      "/pets/{id}",
				},
			},
		},
		{
			name: "via target mismatch",
			args: []string{"allure/allure-deployment@prod#getUsers"},
			flags: RootFlags{
				Target: "shiva",
			},
			expectedErr: "packed @target must match --via",
		},
		{
			name: "refresh offline conflict",
			args: []string{"allure/allure-deployment"},
			flags: RootFlags{
				Refresh: true,
				Offline: true,
			},
			expectedErr: "--refresh and --offline are mutually exclusive",
		},
		{
			name:        "call mode requires operation for single arg selector",
			args:        []string{"allure/allure-deployment@prod"},
			expectedErr: "call mode requires either #<operation-id> or <method> <path>",
		},
		{
			name: "dry run requires call mode",
			args: []string{"allure/allure-deployment"},
			flags: RootFlags{
				DryRun: true,
			},
			expectedErr: "--dry-run requires call mode",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			actual, err := ParseShorthandInvocation(testCase.args, testCase.flags)
			if testCase.expectedErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", testCase.expectedErr)
				}
				if err.Error() != testCase.expectedErr {
					t.Fatalf("expected error %q, got %q", testCase.expectedErr, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("parse shorthand failed: %v", err)
			}
			if !reflect.DeepEqual(actual, testCase.expected) {
				t.Fatalf("expected invocation %+v, got %+v", testCase.expected, actual)
			}
		})
	}
}
