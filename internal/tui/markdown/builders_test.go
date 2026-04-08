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
				"/: Path",
				"* /{petId}/: string",
				"?& Query",
				"&limit: integer = 50",
				"{} Body",
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
			output := normalizeWhitespace(stripANSI(BuildEndpoint(tc.input)))
			for _, part := range tc.contains {
				if !strings.Contains(output, normalizeWhitespace(part)) {
					t.Fatalf("expected endpoint markdown to contain %q; output:\n%s", part, output)
				}
			}
		})
	}
}

func TestBuildRequest(t *testing.T) {
	t.Parallel()

	output := normalizeWhitespace(stripANSI(BuildRequest(EndpointInput{
		Method: "get",
		Path:   "/pets/{petId}",
		Operation: mustRawJSON(t, map[string]any{
			"operationId": "listPets",
			"summary":     "List pets",
			"description": "Returns pets from the catalog.",
			"parameters": []any{
				map[string]any{
					"name":     "petId",
					"in":       "path",
					"required": true,
					"schema": map[string]any{
						"type": "string",
					},
				},
			},
			"requestBody": map[string]any{
				"required": true,
				"content": map[string]any{
					"application/json": map[string]any{
						"schema": map[string]any{
							"type": "object",
						},
					},
				},
			},
			"responses": map[string]any{
				"200": map[string]any{
					"description": "OK",
				},
			},
		}),
	})))

	for _, part := range []string{
		"`Operation ID:` `listPets`",
		"/: Path",
		"{} Body",
	} {
		if !strings.Contains(output, normalizeWhitespace(part)) {
			t.Fatalf("expected request markdown to contain %q; output:\n%s", part, output)
		}
	}
	if strings.Contains(output, "### Responses") {
		t.Fatalf("expected request markdown to exclude responses; output:\n%s", output)
	}
	if strings.Contains(output, "`Summary:`") {
		t.Fatalf("expected request markdown to exclude summary label; output:\n%s", output)
	}
}

func TestBuildRequest_NoDescriptionFallback(t *testing.T) {
	t.Parallel()

	output := BuildRequest(EndpointInput{
		Method: "get",
		Path:   "/pets/{petId}",
		Operation: mustRawJSON(t, map[string]any{
			"operationId": "listPets",
		}),
	})

	if !strings.Contains(output, "No decsription") {
		t.Fatalf("expected request markdown to include missing-description fallback; output:\n%s", output)
	}
}

func TestBuildSuccessResponses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		operation  map[string]any
		contains   []string
		notContain []string
	}{
		{
			name: "keeps 2xx responses only",
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
				"## Responses",
				"#### `200` OK",
				"#### `201` Created",
			},
			notContain: []string{
				"#### `400`",
				"#### `500`",
				"#### `default`",
			},
		},
		{
			name: "returns empty-state when no documented success responses",
			operation: map[string]any{
				"responses": map[string]any{
					"400": map[string]any{
						"description": "Bad request",
					},
				},
			},
			contains: []string{
				"## Responses",
				"No documented 2xx responses.",
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			output := BuildSuccessResponses(mustRawJSON(t, tc.operation))
			for _, part := range tc.contains {
				if !strings.Contains(output, part) {
					t.Fatalf("expected responses markdown to contain %q; output:\n%s", part, output)
				}
			}
			for _, part := range tc.notContain {
				if strings.Contains(output, part) {
					t.Fatalf("expected responses markdown to exclude %q; output:\n%s", part, output)
				}
			}
		})
	}
}

func TestBuildEmptyStateBuilders(t *testing.T) {
	t.Parallel()

	if got := BuildEmptySuccessResponses(); !strings.Contains(got, "No documented 2xx responses.") {
		t.Fatalf("expected responses empty-state text, got:\n%s", got)
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

func stripANSI(value string) string {
	replacer := strings.NewReplacer("\u001b[0m", "")
	value = replacer.Replace(value)
	for {
		start := strings.Index(value, "\u001b[")
		if start < 0 {
			return value
		}
		end := start
		for end < len(value) && value[end] != 'm' {
			end++
		}
		if end >= len(value) {
			return value[:start]
		}
		value = value[:start] + value[end+1:]
	}
}

func normalizeWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
