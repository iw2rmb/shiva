package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	clioutput "github.com/iw2rmb/shiva/internal/cli/output"
)

func TestBuildAPIDataMarkdown(t *testing.T) {
	t.Parallel()

	processedAt := time.Date(2026, time.April, 9, 10, 11, 12, 0, time.UTC)
	testCases := []struct {
		name     string
		selected APIEntry
		detail   *SpecDetail
		expected string
	}{
		{
			name: "renders requested data template",
			selected: APIEntry{
				Row: clioutput.APIRow{
					Status:                 "active",
					APISpecRevisionID:      501,
					IngestEventSHA:         "deadbeefcafebabe",
					IngestEventBranch:      "main",
					IngestEventProcessedAt: &processedAt,
				},
			},
			detail: &SpecDetail{
				Body: json.RawMessage(`{
					"openapi":"3.1.0",
					"info":{"description":"Primary API for pets"},
					"servers":[
						{"description":"Staging","url":"https://staging.example.com"},
						{"description":"Production","url":"https://api.example.com"}
					]
				}`),
			},
			expected: strings.Join([]string{
				"Status: active",
				"Ingest: main (deadbeef) @ 09-04-26 10:11:12",
				"Revision: 501",
				"",
				"Primary API for pets",
				"",
				"Servers:",
				"- Production: https://api.example.com",
				"- Staging: https://staging.example.com",
				"",
				"OpenAPI v3.1.0",
			}, "\n"),
		},
		{
			name: "renders dash placeholders when fields are missing",
			selected: APIEntry{
				Row: clioutput.APIRow{},
			},
			detail: &SpecDetail{},
			expected: strings.Join([]string{
				"Status: -",
				"Ingest: - (-) @ -",
				"Revision: -",
				"",
				"-",
				"",
				"Servers:",
				"- -",
				"",
				"OpenAPI v-",
			}, "\n"),
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := buildAPIDataMarkdown(testCase.selected, testCase.detail)
			if got != testCase.expected {
				t.Fatalf("unexpected markdown\nexpected:\n%s\n\ngot:\n%s", testCase.expected, got)
			}
		})
	}
}
