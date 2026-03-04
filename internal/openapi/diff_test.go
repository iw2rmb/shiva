package openapi

import (
	"reflect"
	"strings"
	"testing"
)

func TestComputeSemanticDiff(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		previous      []EndpointSnapshot
		current       []EndpointSnapshot
		expected      SpecChanges
		expectedError string
	}{
		{
			name: "no baseline yields full additions model",
			current: []EndpointSnapshot{
				{
					Method:  "POST",
					Path:    "/pets",
					RawJSON: []byte(`{"summary":"Create pet","responses":{"201":{"description":"created"}}}`),
				},
				{
					Method:  "get",
					Path:    "/pets/{id}",
					RawJSON: []byte(`{"summary":"Get pet","responses":{"200":{"description":"ok"}}}`),
				},
			},
			expected: SpecChanges{
				Version: SpecChangesVersion,
				Endpoints: EndpointChanges{
					Added: []EndpointRef{
						{Method: "get", Path: "/pets/{id}"},
						{Method: "post", Path: "/pets"},
					},
					Removed: []EndpointRef{},
					Changed: []EndpointChange{},
				},
				Summary: SpecChangesSummary{
					AddedEndpoints:   2,
					RemovedEndpoints: 0,
					ChangedEndpoints: 0,
					ParameterChanges: 0,
					SchemaChanges:    0,
				},
			},
		},
		{
			name: "classifies endpoint parameter schema and operation-only changes",
			previous: []EndpointSnapshot{
				{
					Method: "get",
					Path:   "/pets",
					RawJSON: []byte(`{
						"summary":"List pets",
						"parameters":[
							{"name":"limit","in":"query","schema":{"type":"integer"}},
							{"name":"version","in":"header","schema":{"type":"string"}}
						],
						"requestBody":{
							"content":{
								"application/json":{"schema":{"type":"object","properties":{"name":{"type":"string"}}}}
							}
						},
						"responses":{
							"200":{"content":{"application/json":{"schema":{"type":"object","properties":{"id":{"type":"integer"}}}}}}
						}
					}`),
				},
				{
					Method:  "delete",
					Path:    "/pets/{id}",
					RawJSON: []byte(`{"summary":"Delete pet","responses":{"204":{"description":"deleted"}}}`),
				},
			},
			current: []EndpointSnapshot{
				{
					Method: "GET",
					Path:   "/pets",
					RawJSON: []byte(`{
						"summary":"List active pets",
						"parameters":[
							{"name":"limit","in":"query","schema":{"type":"string"}},
							{"name":"expand","in":"query","schema":{"type":"boolean"}}
						],
						"requestBody":{
							"content":{
								"application/json":{
									"schema":{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}
								}
							}
						},
						"responses":{
							"200":{"content":{"application/json":{"schema":{"type":"object","properties":{"id":{"type":"string"}}}}}},
							"201":{"content":{"application/json":{"schema":{"type":"object","properties":{"status":{"type":"string"}}}}}}
						}
					}`),
				},
				{
					Method:  "DELETE",
					Path:    "/pets/{id}",
					RawJSON: []byte(`{"summary":"Delete a pet","responses":{"204":{"description":"deleted"}}}`),
				},
				{
					Method:  "post",
					Path:    "/pets",
					RawJSON: []byte(`{"responses":{"201":{"description":"created"}}}`),
				},
			},
			expected: SpecChanges{
				Version: SpecChangesVersion,
				Endpoints: EndpointChanges{
					Added: []EndpointRef{
						{Method: "post", Path: "/pets"},
					},
					Removed: []EndpointRef{},
					Changed: []EndpointChange{
						{
							Method:      "delete",
							Path:        "/pets/{id}",
							ChangeTypes: []string{"operation"},
							Parameters: FieldChanges{
								Added:   []string{},
								Removed: []string{},
								Changed: []string{},
							},
							Schemas: FieldChanges{
								Added:   []string{},
								Removed: []string{},
								Changed: []string{},
							},
						},
						{
							Method:      "get",
							Path:        "/pets",
							ChangeTypes: []string{"parameters", "schemas"},
							Parameters: FieldChanges{
								Added:   []string{"query:expand"},
								Removed: []string{"header:version"},
								Changed: []string{"query:limit"},
							},
							Schemas: FieldChanges{
								Added: []string{
									"parameters.query:expand.schema",
									"responses.201.content.application/json.schema",
								},
								Removed: []string{"parameters.header:version.schema"},
								Changed: []string{
									"parameters.query:limit.schema",
									"request_body.content.application/json.schema",
									"responses.200.content.application/json.schema",
								},
							},
						},
					},
				},
				Summary: SpecChangesSummary{
					AddedEndpoints:   1,
					RemovedEndpoints: 0,
					ChangedEndpoints: 2,
					ParameterChanges: 3,
					SchemaChanges:    6,
				},
			},
		},
		{
			name: "rejects duplicate endpoint keys",
			current: []EndpointSnapshot{
				{
					Method:  "GET",
					Path:    "/pets",
					RawJSON: []byte(`{"responses":{"200":{"description":"ok"}}}`),
				},
				{
					Method:  "get",
					Path:    "/pets",
					RawJSON: []byte(`{"responses":{"200":{"description":"still ok"}}}`),
				},
			},
			expectedError: "duplicate endpoint key",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			actual, err := ComputeSemanticDiff(testCase.previous, testCase.current)
			if testCase.expectedError != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", testCase.expectedError)
				}
				if !strings.Contains(err.Error(), testCase.expectedError) {
					t.Fatalf("expected error containing %q, got %q", testCase.expectedError, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("ComputeSemanticDiff() unexpected error: %v", err)
			}

			if !reflect.DeepEqual(actual, testCase.expected) {
				t.Fatalf("semantic diff mismatch\nexpected: %#v\nactual:   %#v", testCase.expected, actual)
			}
		})
	}
}
