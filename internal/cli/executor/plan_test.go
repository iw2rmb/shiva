package executor

import (
	"reflect"
	"testing"

	"github.com/iw2rmb/shiva/internal/cli/request"
)

func TestPlanShivaCall(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		input       request.Envelope
		expected    CallPlan
		expectedErr string
	}{
		{
			name: "defaults target and keeps resolved planning offline",
			input: request.Envelope{
				Kind:        request.KindCall,
				Repo:        "acme/platform",
				API:         "apis/pets/openapi.yaml",
				RevisionID:  42,
				SHA:         "deadbeef",
				OperationID: "listPets",
				Method:      "get",
				Path:        "/pets",
				DryRun:      true,
			},
			expected: CallPlan{
				Request: request.Envelope{
					Kind:        request.KindCall,
					Repo:        "acme/platform",
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
				},
			},
		},
		{
			name: "rejects non-call kind",
			input: request.Envelope{
				Kind: request.KindSpec,
				Repo: "acme/platform",
			},
			expectedErr: `kind must be "call"`,
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			actual, err := PlanShivaCall(testCase.input)
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
				t.Fatalf("plan shiva call failed: %v", err)
			}
			if !reflect.DeepEqual(actual, testCase.expected) {
				t.Fatalf("expected plan %+v, got %+v", testCase.expected, actual)
			}
		})
	}
}
