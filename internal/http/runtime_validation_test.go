package httpserver

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/iw2rmb/shiva/internal/store"
)

const runtimeValidationSpec = `{
  "openapi":"3.1.0",
  "info":{"title":"Pets","version":"1.0.0"},
  "paths":{
    "/pets":{
      "get":{
        "operationId":"listPets",
        "security":[{"authHeader":[]}],
        "parameters":[
          {"name":"limit","in":"query","required":true,"schema":{"type":"integer"}},
          {"name":"X-Mode","in":"header","required":true,"schema":{"type":"string","enum":["full"]}}
        ],
        "responses":{
          "200":{
            "description":"ok",
            "headers":{
              "X-Trace":{"required":true,"schema":{"type":"string","example":"trace-1"}}
            },
            "content":{
              "application/json":{
                "schema":{
                  "type":"object",
                  "required":["id","name"],
                  "properties":{
                    "id":{"type":"integer"},
                    "name":{"type":"string","minLength":1}
                  }
                }
              }
            }
          },
          "400":{
            "description":"bad request",
            "content":{"application/json":{"example":{"error":"bad request"}}}
          },
          "401":{
            "description":"unauthorized",
            "content":{"application/json":{"example":{"error":"auth required"}}}
          },
          "406":{
            "description":"not acceptable",
            "content":{"application/json":{"example":{"error":"not acceptable"}}}
          }
        }
      },
      "post":{
        "operationId":"createPet",
        "security":[{"authHeader":[]}],
        "requestBody":{
          "required":true,
          "content":{
            "application/json":{
              "schema":{
                "type":"object",
                "required":["name"],
                "properties":{"name":{"type":"string"}}
              }
            }
          }
        },
        "responses":{
          "201":{
            "description":"created",
            "content":{"application/json":{"example":{"id":1,"name":"Kitty"}}}
          },
          "415":{
            "description":"unsupported media type",
            "content":{"application/json":{"example":{"error":"unsupported media type"}}}
          },
          "422":{
            "description":"unprocessable",
            "content":{"application/json":{"example":{"error":"unprocessable"}}}
          }
        }
      }
    }
  },
  "components":{
    "securitySchemes":{
      "authHeader":{"type":"apiKey","in":"header","name":"X-Auth"}
    }
  }
}`

const runtimePathParamSpec = `{
  "openapi":"3.1.0",
  "info":{"title":"Pets","version":"1.0.0"},
  "paths":{
    "/pets/{id}":{
      "get":{
        "operationId":"getPet",
        "parameters":[
          {"name":"id","in":"path","required":true,"schema":{"type":"integer"}}
        ],
        "responses":{
          "200":{
            "description":"ok",
            "content":{
              "application/json":{
                "example":{"id":7,"name":"Fido"}
              }
            }
          },
          "404":{
            "description":"not found",
            "content":{"application/json":{"example":{"error":"not found"}}}
          }
        }
      }
    }
  }
}`

const runtimeUndocumentedValidationSpec = `{
  "openapi":"3.1.0",
  "info":{"title":"Pets","version":"1.0.0"},
  "paths":{
    "/pets":{
      "post":{
        "operationId":"createPet",
        "requestBody":{
          "required":true,
          "content":{
            "application/json":{
              "schema":{
                "type":"object",
                "required":["name"],
                "properties":{"name":{"type":"string"}}
              }
            }
          }
        },
        "responses":{
          "201":{
            "description":"created",
            "content":{"application/json":{"example":{"id":1,"name":"Kitty"}}}
          }
        }
      }
    }
  }
}`

func TestRuntimeRouteHandler_ValidatesRequestsAndBuildsStubResponses(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		method         string
		target         string
		headers        map[string]string
		body           string
		expectedStatus int
		expectedBody   any
		expectedHeader map[string]string
	}{
		{
			name:   "missing auth returns documented 401",
			method: http.MethodGet,
			target: "/gl/acme/platform/pets?limit=2",
			headers: map[string]string{
				"X-Mode": "full",
			},
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   map[string]any{"error": "auth required"},
		},
		{
			name:   "invalid query returns documented 400",
			method: http.MethodGet,
			target: "/gl/acme/platform/pets?limit=oops",
			headers: map[string]string{
				"X-Auth": "token",
				"X-Mode": "full",
			},
			expectedStatus: http.StatusBadRequest,
			expectedBody:   map[string]any{"error": "bad request"},
		},
		{
			name:   "unsupported media type returns documented 415",
			method: http.MethodPost,
			target: "/gl/acme/platform/pets",
			headers: map[string]string{
				"Content-Type": "text/plain",
				"X-Auth":       "token",
			},
			body:           `plain-text`,
			expectedStatus: http.StatusUnsupportedMediaType,
			expectedBody:   map[string]any{"error": "unsupported media type"},
		},
		{
			name:   "invalid request body returns documented 422",
			method: http.MethodPost,
			target: "/gl/acme/platform/pets",
			headers: map[string]string{
				"Content-Type": "application/json",
				"X-Auth":       "token",
			},
			body:           `{"kind":"cat"}`,
			expectedStatus: http.StatusUnprocessableEntity,
			expectedBody:   map[string]any{"error": "unprocessable"},
		},
		{
			name:   "accept mismatch returns documented 406",
			method: http.MethodGet,
			target: "/gl/acme/platform/pets?limit=2",
			headers: map[string]string{
				"Accept": "text/plain",
				"X-Auth": "token",
				"X-Mode": "full",
			},
			expectedStatus: http.StatusNotAcceptable,
			expectedBody:   map[string]any{"error": "not acceptable"},
		},
		{
			name:   "valid request returns generated stub response",
			method: http.MethodGet,
			target: "/gl/acme/platform/pets?limit=2",
			headers: map[string]string{
				"Accept": "application/json",
				"X-Auth": "token",
				"X-Mode": "full",
			},
			expectedStatus: http.StatusOK,
			expectedBody: map[string]any{
				"id":   float64(0),
				"name": "a",
			},
			expectedHeader: map[string]string{
				"Content-Type": "application/json",
				"X-Trace":      "trace-1",
			},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			server := newQueryTestServer(runtimeValidationReadStore(strings.ToLower(testCase.method)))
			request := httptest.NewRequest(testCase.method, testCase.target, bytes.NewBufferString(testCase.body))
			for key, value := range testCase.headers {
				request.Header.Set(key, value)
			}

			response, err := server.App().Test(request, -1)
			if err != nil {
				t.Fatalf("http test request failed: %v", err)
			}
			defer response.Body.Close()

			if response.StatusCode != testCase.expectedStatus {
				body, _ := io.ReadAll(response.Body)
				t.Fatalf("expected status %d, got %d body=%s", testCase.expectedStatus, response.StatusCode, string(body))
			}

			for key, expected := range testCase.expectedHeader {
				if actual := response.Header.Get(key); actual != expected {
					t.Fatalf("expected header %s=%q, got %q", key, expected, actual)
				}
			}

			var actualBody any
			if err := json.NewDecoder(response.Body).Decode(&actualBody); err != nil {
				t.Fatalf("decode response body: %v", err)
			}
			if !reflect.DeepEqual(actualBody, testCase.expectedBody) {
				t.Fatalf("unexpected response body: %+v", actualBody)
			}
		})
	}
}

func TestRuntimeRouteHandler_ValidatesPathParams(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		target         string
		expectedStatus int
		expectedBody   map[string]any
	}{
		{
			name:           "invalid path param type returns documented 404",
			target:         "/gl/acme/platform/pets/not-a-number",
			expectedStatus: http.StatusNotFound,
			expectedBody:   map[string]any{"error": "not found"},
		},
		{
			name:           "valid path param returns stub response",
			target:         "/gl/acme/platform/pets/7",
			expectedStatus: http.StatusOK,
			expectedBody:   map[string]any{"id": float64(7), "name": "Fido"},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			server := newQueryTestServer(runtimeValidationReadStoreWithSpec(
				http.MethodGet,
				"/pets/{id}",
				runtimePathParamSpec,
			))
			request := httptest.NewRequest(http.MethodGet, testCase.target, nil)

			response, err := server.App().Test(request, -1)
			if err != nil {
				t.Fatalf("http test request failed: %v", err)
			}
			defer response.Body.Close()

			if response.StatusCode != testCase.expectedStatus {
				body, _ := io.ReadAll(response.Body)
				t.Fatalf("expected status %d, got %d body=%s", testCase.expectedStatus, response.StatusCode, string(body))
			}

			var actualBody map[string]any
			if err := json.NewDecoder(response.Body).Decode(&actualBody); err != nil {
				t.Fatalf("decode response body: %v", err)
			}
			if !reflect.DeepEqual(actualBody, testCase.expectedBody) {
				t.Fatalf("unexpected response body: %+v", actualBody)
			}
		})
	}
}

func TestRuntimeRouteHandler_FallsBackTo400ForUndocumentedValidationErrors(t *testing.T) {
	t.Parallel()

	server := newQueryTestServer(runtimeValidationReadStoreWithSpec(
		http.MethodPost,
		"/pets",
		runtimeUndocumentedValidationSpec,
	))
	request := httptest.NewRequest(http.MethodPost, "/gl/acme/platform/pets", bytes.NewBufferString(`{"name":`))
	request.Header.Set("Content-Type", "application/json")

	response, err := server.App().Test(request, -1)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("expected status 400, got %d body=%s", response.StatusCode, string(body))
	}

	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}

	errorValue, ok := body["error"].(string)
	if !ok || strings.TrimSpace(errorValue) == "" {
		t.Fatalf("expected fallback error message, got %+v", body)
	}
}

func runtimeValidationReadStore(method string) *fakeQueryReadStore {
	return runtimeValidationReadStoreWithSpec(method, "/pets", runtimeValidationSpec)
}

func runtimeValidationReadStoreWithSpec(method string, path string, spec string) *fakeQueryReadStore {
	return &fakeQueryReadStore{
		repoLookupResultByPath: map[string]store.Repo{
			"acme/platform": {ID: 77, Namespace: "acme", Repo: "platform", DefaultBranch: "main"},
		},
		resolveReadSnapshotResult: store.ResolvedReadSnapshot{
			Repo:     store.Repo{ID: 77, Namespace: "acme", Repo: "platform", DefaultBranch: "main"},
			Revision: store.Revision{ID: 42, Sha: "deadbeef", Branch: "main"},
		},
		resolveOperationByMethodPathResult: store.ResolvedOperationCandidates{
			Snapshot: store.ResolvedReadSnapshot{
				Repo:     store.Repo{ID: 77, Namespace: "acme", Repo: "platform", DefaultBranch: "main"},
				Revision: store.Revision{ID: 42, Sha: "deadbeef", Branch: "main"},
			},
			Candidates: []store.OperationSnapshot{
				{
					API:               "apis/pets/openapi.yaml",
					APISpecRevisionID: 501,
					IngestEventID:     42,
					IngestEventSHA:    "deadbeef",
					IngestEventBranch: "main",
					Method:            strings.ToLower(method),
					Path:              path,
					OperationID:       "runtimeOperation",
					RawJSON:           []byte(`{"operationId":"runtimeOperation"}`),
				},
			},
		},
		specArtifactResult: store.SpecArtifact{
			APISpecRevisionID: 501,
			SpecJSON:          []byte(spec),
		},
	}
}
