package cli

import (
	"context"
	"encoding/json"
	"testing"
)

func TestDraftServiceResolveSingleActiveAPI(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		listings    []APISpecListing
		expectedAPI string
		expectedErr string
	}{
		{
			name: "single active api",
			listings: []APISpecListing{
				{API: "service-catalog/allure-api.yaml", Status: "active"},
				{API: "service-catalog/old-api.yaml", Status: "deleted"},
			},
			expectedAPI: "service-catalog/allure-api.yaml",
		},
		{
			name: "no active apis",
			listings: []APISpecListing{
				{API: "service-catalog/old-api.yaml", Status: "deleted"},
			},
			expectedErr: `repo "allure/allure-deployment" has no active api specs`,
		},
		{
			name: "multiple active apis",
			listings: []APISpecListing{
				{API: "service-catalog/allure-api.yaml", Status: "active"},
				{API: "service-catalog/admin-api.yaml", Status: "active"},
			},
			expectedErr: `repo "allure/allure-deployment" has multiple active apis; draft CLI requires exactly one: service-catalog/allure-api.yaml, service-catalog/admin-api.yaml`,
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			service := &DraftService{
				client: fakeSpecClient{
					listings: testCase.listings,
				},
			}

			actualAPI, err := service.resolveSingleActiveAPI(context.Background(), "allure/allure-deployment")
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
				t.Fatalf("resolve single active api failed: %v", err)
			}
			if actualAPI != testCase.expectedAPI {
				t.Fatalf("expected api %q, got %q", testCase.expectedAPI, actualAPI)
			}
		})
	}
}

func TestDraftServiceGetRepoSpecUsesResolvedAPI(t *testing.T) {
	t.Parallel()

	client := &recordingSpecClient{
		listings: []APISpecListing{
			{API: "service-catalog/allure-api.yaml", Status: "active"},
		},
		specBody: []byte("openapi: 3.1.0\npaths: {}\n"),
	}

	service := &DraftService{client: client}
	body, err := service.GetRepoSpec(context.Background(), "allure/allure-deployment")
	if err != nil {
		t.Fatalf("get repo spec failed: %v", err)
	}
	if string(body) != "openapi: 3.1.0\npaths: {}\n" {
		t.Fatalf("unexpected repo spec body: %q", string(body))
	}
	if client.lastRepoPath != "allure/allure-deployment" {
		t.Fatalf("expected repo path to be recorded, got %q", client.lastRepoPath)
	}
	if client.lastAPIRoot != "service-catalog/allure-api.yaml" {
		t.Fatalf("expected api root to be recorded, got %q", client.lastAPIRoot)
	}
	if client.lastFormat != SpecFormatYAML {
		t.Fatalf("expected yaml format, got %q", client.lastFormat)
	}
}

func TestEncodeDelimitedPath(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "preserves path separators",
			input:    "service-catalog/allure-api.yaml",
			expected: "service-catalog/allure-api.yaml",
		},
		{
			name:     "escapes segment content",
			input:    "api docs/spec file.yaml",
			expected: "api%20docs/spec%20file.yaml",
		},
		{
			name:     "drops empty segments",
			input:    "/api//specs/openapi.yaml/",
			expected: "api/specs/openapi.yaml",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			actual := encodeDelimitedPath(testCase.input)
			if actual != testCase.expected {
				t.Fatalf("expected encoded path %q, got %q", testCase.expected, actual)
			}
		})
	}
}

func TestOperationPayloadByID(t *testing.T) {
	t.Parallel()

	specJSONWithUniqueOperation := []byte(`{
		"openapi":"3.1.0",
		"paths":{
			"/pets":{"get":{"operationId":"listPets","summary":"List pets","responses":{"200":{"description":"ok"}}}},
			"/pets/{id}":{"get":{"operationId":"getPet","responses":{"200":{"description":"ok"}}}}
		}
	}`)
	specJSONWithDuplicateOperation := []byte(`{
		"openapi":"3.1.0",
		"paths":{
			"/pets":{"get":{"operationId":"listPets","responses":{"200":{"description":"ok"}}}},
			"/pets/search":{"post":{"operationId":"listPets","responses":{"200":{"description":"ok"}}}}
		}
	}`)

	testCases := []struct {
		name            string
		specJSON        []byte
		operationID     string
		expectedPath    string
		expectedMethod  string
		expectedErr     string
		expectedSummary string
	}{
		{
			name:            "unique operation id",
			specJSON:        specJSONWithUniqueOperation,
			operationID:     "listPets",
			expectedPath:    "/pets",
			expectedMethod:  "get",
			expectedSummary: "List pets",
		},
		{
			name:        "missing operation id",
			specJSON:    specJSONWithUniqueOperation,
			operationID: "createPet",
			expectedErr: `operation "createPet" was not found in repo "allure/allure-deployment"`,
		},
		{
			name:        "duplicate operation id",
			specJSON:    specJSONWithDuplicateOperation,
			operationID: "listPets",
			expectedErr: `operation "listPets" in repo "allure/allure-deployment" matched multiple endpoints: get /pets, post /pets/search`,
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			body, err := operationPayloadByID("allure/allure-deployment", testCase.operationID, testCase.specJSON)
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
				t.Fatalf("operation payload lookup failed: %v", err)
			}

			var payload map[string]map[string]map[string]map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("unmarshal operation payload failed: %v", err)
			}

			methodMap, ok := payload["paths"][testCase.expectedPath]
			if !ok {
				t.Fatalf("expected path %q in payload, got %#v", testCase.expectedPath, payload)
			}

			operation, ok := methodMap[testCase.expectedMethod]
			if !ok {
				t.Fatalf("expected method %q in payload, got %#v", testCase.expectedMethod, methodMap)
			}

			summary, _ := operation["summary"].(string)
			if summary != testCase.expectedSummary {
				t.Fatalf("expected summary %q, got %q", testCase.expectedSummary, summary)
			}
		})
	}
}

type fakeSpecClient struct {
	listings []APISpecListing
}

func (c fakeSpecClient) ListAPISpecs(ctx context.Context, repoPath string) ([]APISpecListing, error) {
	return c.listings, nil
}

func (c fakeSpecClient) GetSpec(ctx context.Context, repoPath string, apiRoot string, format SpecFormat) ([]byte, error) {
	return nil, nil
}

type recordingSpecClient struct {
	listings     []APISpecListing
	specBody     []byte
	lastRepoPath string
	lastAPIRoot  string
	lastFormat   SpecFormat
}

func (c *recordingSpecClient) ListAPISpecs(ctx context.Context, repoPath string) ([]APISpecListing, error) {
	return c.listings, nil
}

func (c *recordingSpecClient) GetSpec(ctx context.Context, repoPath string, apiRoot string, format SpecFormat) ([]byte, error) {
	c.lastRepoPath = repoPath
	c.lastAPIRoot = apiRoot
	c.lastFormat = format
	return c.specBody, nil
}
