package cli

import "testing"

func TestParseSelector(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		input       string
		expected    Selector
		expectedErr string
	}{
		{
			name:  "repo only",
			input: "allure/allure-deployment",
			expected: Selector{
				RepoPath: "allure/allure-deployment",
			},
		},
		{
			name:  "repo and operation",
			input: "allure/allure-deployment#findAll_42",
			expected: Selector{
				RepoPath:    "allure/allure-deployment",
				OperationID: "findAll_42",
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
			name:        "empty operation",
			input:       "allure/allure-deployment#",
			expectedErr: "operation id must not be empty",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			actual, err := ParseSelector(testCase.input)
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
