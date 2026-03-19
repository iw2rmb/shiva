package markdown

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildEndpoint(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		input    EndpointInput
		contains []string
	}{
		{
			name: "includes method path params body and responses",
			input: EndpointInput{
				Method: "get",
				Path:   "/pets/{petId}",
				Operation: mustRawJSON(t, map[string]any{
					"operationId": "listPets",
					"summary":     "List pets",
					"description": "Returns pets from the catalog.",
					"parameters": []any{
						map[string]any{
							"name":        "request-id",
							"in":          "header",
							"required":    false,
							"description": "Request id.",
							"schema": map[string]any{
								"type": "string",
							},
						},
						map[string]any{
							"name":        "petId",
							"in":          "path",
							"required":    true,
							"description": "Pet identifier.",
							"schema": map[string]any{
								"type": "string",
							},
						},
						map[string]any{
							"name":        "limit",
							"in":          "query",
							"required":    false,
							"description": "Page size.",
							"example":     50,
							"schema": map[string]any{
								"type": "integer",
							},
						},
					},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{
									"title": "CreatePetRequest",
									"type":  "object",
									"required": []any{
										"name",
									},
									"properties": map[string]any{
										"name": map[string]any{
											"type":        "string",
											"description": "Pet name.",
										},
										"tag": map[string]any{
											"type": "string",
										},
									},
								},
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Success response.",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{
										"title": "ListPetsResponse",
										"type":  "object",
										"properties": map[string]any{
											"items": map[string]any{
												"type": "array",
												"items": map[string]any{
													"type": "string",
												},
											},
										},
									},
								},
							},
						},
						"400": map[string]any{
							"description": "Bad request.",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{
										"title": "BadRequestError",
										"type":  "object",
										"required": []any{
											"message",
										},
										"properties": map[string]any{
											"message": map[string]any{
												"type": "string",
											},
										},
									},
								},
							},
						},
					},
				}),
			},
			contains: []string{
				"## GET /pets/{petId}",
				"`Operation ID:` `listPets`",
				"### Parameters",
				"#### Path Parameters",
				"* /{petId}/: string",
				"#### Query Parameters",
				"&limit: integer = 50",
				"### Request Body",
				"#### `application/json`",
				"CreatePetRequest {",
				"### Responses",
				"#### `200` Success response.",
				"#### `400` Bad request.",
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			output := BuildEndpoint(tc.input)
			for _, part := range tc.contains {
				if !strings.Contains(output, part) {
					t.Fatalf("expected endpoint markdown to contain %q; output:\n%s", part, output)
				}
			}
		})
	}
}

func TestBuildServers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		operation  map[string]any
		spec       map[string]any
		contains   []string
		notContain []string
	}{
		{
			name: "operation servers override spec servers",
			operation: map[string]any{
				"servers": []any{
					map[string]any{
						"url":         "https://operation.example.com",
						"description": "Operation server",
					},
				},
			},
			spec: map[string]any{
				"servers": []any{
					map[string]any{
						"url": "https://spec.example.com",
					},
				},
			},
			contains: []string{
				"## Servers",
				"`Source:` `operation`",
				"https://operation.example.com",
			},
			notContain: []string{
				"https://spec.example.com",
				"`Source:` `spec`",
			},
		},
		{
			name: "spec servers used when operation has none",
			operation: map[string]any{
				"operationId": "listPets",
			},
			spec: map[string]any{
				"servers": []any{
					map[string]any{
						"url":         "https://spec.example.com",
						"description": "Spec server",
					},
				},
			},
			contains: []string{
				"`Source:` `spec`",
				"https://spec.example.com",
			},
		},
		{
			name: "empty state when no servers are documented",
			operation: map[string]any{
				"operationId": "listPets",
			},
			spec: map[string]any{
				"openapi": "3.1.0",
			},
			contains: []string{
				"## Servers",
				"No servers documented for this endpoint or API spec.",
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			output := BuildServers(mustRawJSON(t, tc.operation), mustRawJSON(t, tc.spec))
			for _, part := range tc.contains {
				if !strings.Contains(output, part) {
					t.Fatalf("expected servers markdown to contain %q; output:\n%s", part, output)
				}
			}
			for _, part := range tc.notContain {
				if strings.Contains(output, part) {
					t.Fatalf("expected servers markdown to exclude %q; output:\n%s", part, output)
				}
			}
		})
	}
}

func TestBuildErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		operation  map[string]any
		contains   []string
		notContain []string
	}{
		{
			name: "keeps non-2xx and default only",
			operation: map[string]any{
				"responses": map[string]any{
					"200": map[string]any{
						"description": "OK",
					},
					"201": map[string]any{
						"description": "Created",
					},
					"400": map[string]any{
						"description": "Bad request",
					},
					"500": map[string]any{
						"description": "Unexpected",
					},
					"default": map[string]any{
						"description": "Generic error",
					},
				},
			},
			contains: []string{
				"## Errors",
				"#### `400` Bad request",
				"#### `500` Unexpected",
				"#### `default` Generic error",
			},
			notContain: []string{
				"#### `200`",
				"#### `201`",
			},
		},
		{
			name: "returns empty-state when no documented errors",
			operation: map[string]any{
				"responses": map[string]any{
					"200": map[string]any{
						"description": "OK",
					},
				},
			},
			contains: []string{
				"## Errors",
				"No documented non-2xx or default responses.",
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			output := BuildErrors(mustRawJSON(t, tc.operation))
			for _, part := range tc.contains {
				if !strings.Contains(output, part) {
					t.Fatalf("expected errors markdown to contain %q; output:\n%s", part, output)
				}
			}
			for _, part := range tc.notContain {
				if strings.Contains(output, part) {
					t.Fatalf("expected errors markdown to exclude %q; output:\n%s", part, output)
				}
			}
		})
	}
}

func TestBuildEmptyStateBuilders(t *testing.T) {
	t.Parallel()

	if got := BuildEmptyServers(); !strings.Contains(got, "No servers documented for this endpoint or API spec.") {
		t.Fatalf("expected servers empty-state text, got:\n%s", got)
	}
	if got := BuildEmptyErrors(); !strings.Contains(got, "No documented non-2xx or default responses.") {
		t.Fatalf("expected errors empty-state text, got:\n%s", got)
	}
}

func mustRawJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return body
}
