package request

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestNormalizeCallEnvelope(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		input       Envelope
		options     NormalizeCallOptions
		expected    Envelope
		expectedErr string
	}{
		{
			name: "operation id selector with default target",
			input: Envelope{
				Repo:        " acme/platform ",
				OperationID: "listPets",
				PathParams:  map[string]string{"id": "42"},
				QueryParams: map[string][]string{"expand": []string{"owners"}},
				Headers:     map[string][]string{"X-Trace": []string{"abc"}},
				JSONBody:    json.RawMessage(`{"ok":true}`),
				DryRun:      true,
			},
			options: NormalizeCallOptions{
				DefaultTarget:    DefaultShivaTarget,
				AllowMissingKind: true,
			},
			expected: Envelope{
				Kind:        KindCall,
				Repo:        "acme/platform",
				Target:      DefaultShivaTarget,
				OperationID: "listPets",
				PathParams:  map[string]string{"id": "42"},
				QueryParams: map[string][]string{"expand": []string{"owners"}},
				Headers:     map[string][]string{"X-Trace": []string{"abc"}},
				JSONBody:    json.RawMessage(`{"ok":true}`),
				DryRun:      true,
			},
		},
		{
			name: "method path selector normalizes casing and leading slash",
			input: Envelope{
				Kind:   KindCall,
				Repo:   "acme/platform",
				API:    "apis/pets.yaml",
				Method: "PATCH",
				Path:   "pets/{id}",
			},
			options: NormalizeCallOptions{},
			expected: Envelope{
				Kind:   KindCall,
				Repo:   "acme/platform",
				API:    "apis/pets.yaml",
				Method: "patch",
				Path:   "/pets/{id}",
			},
		},
		{
			name: "rejects invalid selector combination",
			input: Envelope{
				Kind:        KindCall,
				Repo:        "acme/platform",
				OperationID: "listPets",
				Method:      "get",
				Path:        "/pets",
			},
			options:     NormalizeCallOptions{},
			expectedErr: "operation_id is mutually exclusive with method and path",
		},
		{
			name: "rejects invalid json and raw body combination",
			input: Envelope{
				Kind:     KindCall,
				Repo:     "acme/platform",
				Method:   "get",
				Path:     "/pets",
				JSONBody: json.RawMessage(`{"ok":true}`),
				Body:     "raw",
			},
			options:     NormalizeCallOptions{},
			expectedErr: "json and body are mutually exclusive",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			actual, err := NormalizeCallEnvelope(testCase.input, testCase.options)
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
				t.Fatalf("normalize call envelope failed: %v", err)
			}
			if !reflect.DeepEqual(actual, testCase.expected) {
				t.Fatalf("expected envelope %+v, got %+v", testCase.expected, actual)
			}
		})
	}
}

func TestNormalizeSnapshotSelectorRejectsInvalidRevisionSelector(t *testing.T) {
	t.Parallel()

	_, _, _, _, err := NormalizeSnapshotSelector("acme/platform", "", 12, "deadbeef")
	if err == nil {
		t.Fatal("expected revision selector error, got nil")
	}
	if err.Error() != "revision_id and sha are mutually exclusive" {
		t.Fatalf("unexpected error %q", err.Error())
	}
}

func TestNormalizeEnvelope(t *testing.T) {
	t.Parallel()

	t.Run("operation envelope reuses inspect selector normalization", func(t *testing.T) {
		t.Parallel()

		actual, err := NormalizeEnvelope(Envelope{
			Kind:   KindOperation,
			Repo:   "acme/platform",
			Method: "PATCH",
			Path:   "pets/{id}",
		}, NormalizeOptions{DefaultKind: KindOperation})
		if err != nil {
			t.Fatalf("normalize envelope failed: %v", err)
		}

		expected := Envelope{
			Kind:   KindOperation,
			Repo:   "acme/platform",
			Method: "patch",
			Path:   "/pets/{id}",
		}
		if !reflect.DeepEqual(actual, expected) {
			t.Fatalf("expected envelope %+v, got %+v", expected, actual)
		}
	})

	t.Run("spec envelope rejects call-only input", func(t *testing.T) {
		t.Parallel()

		_, err := NormalizeEnvelope(Envelope{
			Kind:   KindSpec,
			Repo:   "acme/platform",
			Target: "shiva",
		}, NormalizeOptions{DefaultKind: KindSpec})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if err.Error() != `call inputs are not supported for kind "spec"` {
			t.Fatalf("unexpected error %q", err.Error())
		}
	})
}
