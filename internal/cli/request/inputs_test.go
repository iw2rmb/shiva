package request

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestApplyCLIInputs(t *testing.T) {
	t.Parallel()

	jsonPath := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(jsonPath, []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatalf("write json fixture: %v", err)
	}

	bodyPath := filepath.Join(t.TempDir(), "payload.txt")
	if err := os.WriteFile(bodyPath, []byte("raw-body"), 0o644); err != nil {
		t.Fatalf("write body fixture: %v", err)
	}

	testCases := []struct {
		name        string
		envelope    Envelope
		inputs      CLIInputs
		expected    Envelope
		expectedErr string
	}{
		{
			name: "applies repeated path query header and json inputs",
			envelope: Envelope{
				Kind:        KindCall,
				Repo:        "acme/platform",
				Target:      "prod",
				OperationID: "listPets",
			},
			inputs: CLIInputs{
				Path:   []string{"id=42"},
				Query:  []string{"expand=owners", "expand=metrics"},
				Header: []string{"X-Trace=abc", "X-Trace=def"},
				JSON:   "@" + jsonPath,
			},
			expected: Envelope{
				Kind:        KindCall,
				Repo:        "acme/platform",
				Target:      "prod",
				OperationID: "listPets",
				PathParams:  map[string]string{"id": "42"},
				QueryParams: map[string][]string{"expand": []string{"owners", "metrics"}},
				Headers:     map[string][]string{"X-Trace": []string{"abc", "def"}},
				JSONBody:    json.RawMessage(`{"ok":true}`),
			},
		},
		{
			name: "loads body from file",
			envelope: Envelope{
				Kind:   KindCall,
				Repo:   "acme/platform",
				Target: "prod",
				Method: "get",
				Path:   "/pets",
			},
			inputs: CLIInputs{
				Body: "@" + bodyPath,
			},
			expected: Envelope{
				Kind:   KindCall,
				Repo:   "acme/platform",
				Target: "prod",
				Method: "get",
				Path:   "/pets",
				Body:   "raw-body",
			},
		},
		{
			name: "rejects call inputs for inspect mode",
			envelope: Envelope{
				Kind: KindSpec,
				Repo: "acme/platform",
			},
			inputs: CLIInputs{
				Path: []string{"id=42"},
			},
			expectedErr: "call input flags require call mode",
		},
		{
			name: "rejects duplicate path params",
			envelope: Envelope{
				Kind:        KindCall,
				Repo:        "acme/platform",
				Target:      "prod",
				OperationID: "listPets",
			},
			inputs: CLIInputs{
				Path: []string{"id=42", "id=43"},
			},
			expectedErr: `path "id" is duplicated`,
		},
		{
			name: "rejects non file body",
			envelope: Envelope{
				Kind:        KindCall,
				Repo:        "acme/platform",
				Target:      "prod",
				OperationID: "listPets",
			},
			inputs: CLIInputs{
				Body: "inline",
			},
			expectedErr: "body must use @file",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			actual, err := ApplyCLIInputs(testCase.envelope, testCase.inputs)
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
				t.Fatalf("apply cli inputs failed: %v", err)
			}
			if !reflect.DeepEqual(actual, testCase.expected) {
				t.Fatalf("expected envelope %+v, got %+v", testCase.expected, actual)
			}
		})
	}
}
