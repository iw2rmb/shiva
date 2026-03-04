package openapi

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestBuildCanonicalSpec_IdenticalInputProducesStableOutput(t *testing.T) {
	t.Parallel()

	resultA := ResolutionResult{
		OpenAPIChanged: true,
		CandidateFiles: []string{"spec/openapi.yaml"},
		Documents: map[string][]byte{
			"spec/openapi.yaml": []byte(
				"openapi: 3.1.0\n" +
					"info:\n" +
					"  title: Demo\n" +
					"  version: 1.0.0\n" +
					"paths:\n" +
					"  /pets:\n" +
					"    get:\n" +
					"      operationId: listPets\n" +
					"      summary: List pets\n" +
					"      responses:\n" +
					"        '200':\n" +
					"          description: ok\n" +
					"          content:\n" +
					"            application/json:\n" +
					"              schema:\n" +
					"                $ref: ./components.yaml#/components/schemas/PetList\n",
			),
			"spec/components.yaml": []byte(
				"components:\n" +
					"  schemas:\n" +
					"    PetList:\n" +
					"      type: array\n" +
					"      items:\n" +
					"        $ref: ./models/pet.yaml#/Pet\n",
			),
			"spec/models/pet.yaml": []byte(
				"Pet:\n" +
					"  type: object\n" +
					"  properties:\n" +
					"    name:\n" +
					"      type: string\n" +
					"    id:\n" +
					"      type: string\n",
			),
		},
	}

	resultB := ResolutionResult{
		OpenAPIChanged: true,
		CandidateFiles: []string{"spec/openapi.yaml"},
		Documents: map[string][]byte{
			"spec/models/pet.yaml": []byte(
				"Pet:\n" +
					"  type: object\n" +
					"  properties:\n" +
					"    id:\n" +
					"      type: string\n" +
					"    name:\n" +
					"      type: string\n",
			),
			"spec/components.yaml": []byte(
				"components:\n" +
					"  schemas:\n" +
					"    PetList:\n" +
					"      items:\n" +
					"        $ref: ./models/pet.yaml#/Pet\n" +
					"      type: array\n",
			),
			"spec/openapi.yaml": []byte(
				"openapi: 3.1.0\n" +
					"info:\n" +
					"  version: 1.0.0\n" +
					"  title: Demo\n" +
					"paths:\n" +
					"  /pets:\n" +
					"    get:\n" +
					"      summary: List pets\n" +
					"      operationId: listPets\n" +
					"      responses:\n" +
					"        '200':\n" +
					"          content:\n" +
					"            application/json:\n" +
					"              schema:\n" +
					"                $ref: ./components.yaml#/components/schemas/PetList\n" +
					"          description: ok\n",
			),
		},
	}

	canonicalA, err := BuildCanonicalSpec(resultA)
	if err != nil {
		t.Fatalf("BuildCanonicalSpec(resultA) unexpected error: %v", err)
	}

	canonicalB, err := BuildCanonicalSpec(resultB)
	if err != nil {
		t.Fatalf("BuildCanonicalSpec(resultB) unexpected error: %v", err)
	}

	if !bytes.Equal(canonicalA.SpecJSON, canonicalB.SpecJSON) {
		t.Fatalf("canonical json mismatch\nA: %s\nB: %s", canonicalA.SpecJSON, canonicalB.SpecJSON)
	}
	if canonicalA.SpecYAML != canonicalB.SpecYAML {
		t.Fatalf("canonical yaml mismatch\nA:\n%s\nB:\n%s", canonicalA.SpecYAML, canonicalB.SpecYAML)
	}
	if canonicalA.ETag != canonicalB.ETag {
		t.Fatalf("etag mismatch: %s vs %s", canonicalA.ETag, canonicalB.ETag)
	}
	if canonicalA.SizeBytes != canonicalB.SizeBytes {
		t.Fatalf("size bytes mismatch: %d vs %d", canonicalA.SizeBytes, canonicalB.SizeBytes)
	}
	if !reflect.DeepEqual(canonicalA.Endpoints, canonicalB.Endpoints) {
		t.Fatalf("endpoint extraction mismatch: %#v vs %#v", canonicalA.Endpoints, canonicalB.Endpoints)
	}

	var root map[string]any
	if err := json.Unmarshal(canonicalA.SpecJSON, &root); err != nil {
		t.Fatalf("unmarshal canonical spec json: %v", err)
	}

	paths := root["paths"].(map[string]any)
	getOperation := paths["/pets"].(map[string]any)["get"].(map[string]any)
	response := getOperation["responses"].(map[string]any)["200"].(map[string]any)
	schema := response["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	items := schema["items"].(map[string]any)
	properties := items["properties"].(map[string]any)

	if schema["type"] != "array" {
		t.Fatalf("expected inlined schema type=array, got %#v", schema["type"])
	}
	if properties["id"].(map[string]any)["type"] != "string" {
		t.Fatalf("expected inlined id type=string, got %#v", properties["id"])
	}
}

func TestBuildCanonicalSpec_ExtractsEndpointIndex(t *testing.T) {
	t.Parallel()

	result := ResolutionResult{
		OpenAPIChanged: true,
		CandidateFiles: []string{"api/openapi.yaml"},
		Documents: map[string][]byte{
			"api/openapi.yaml": []byte(
				"openapi: 3.0.3\n" +
					"info:\n" +
					"  title: Endpoint Index Demo\n" +
					"  version: 1.0.0\n" +
					"paths:\n" +
					"  /pets:\n" +
					"    get:\n" +
					"      operationId: listPets\n" +
					"      summary: List pets\n" +
					"      responses:\n" +
					"        '200':\n" +
					"          description: ok\n" +
					"  /pets/{id}:\n" +
					"    delete:\n" +
					"      deprecated: true\n" +
					"      responses:\n" +
					"        '204':\n" +
					"          description: deleted\n",
			),
		},
	}

	canonical, err := BuildCanonicalSpec(result)
	if err != nil {
		t.Fatalf("BuildCanonicalSpec() unexpected error: %v", err)
	}

	if len(canonical.Endpoints) != 2 {
		t.Fatalf("expected 2 endpoints, got %d", len(canonical.Endpoints))
	}

	first := canonical.Endpoints[0]
	if first.Method != "delete" || first.Path != "/pets/{id}" {
		t.Fatalf("unexpected first endpoint ordering: %#v", first)
	}
	if !first.Deprecated {
		t.Fatalf("expected delete endpoint to be deprecated")
	}
	if first.OperationID != "" {
		t.Fatalf("expected empty operation id, got %q", first.OperationID)
	}
	if first.Summary != "" {
		t.Fatalf("expected empty summary, got %q", first.Summary)
	}

	second := canonical.Endpoints[1]
	if second.Method != "get" || second.Path != "/pets" {
		t.Fatalf("unexpected second endpoint ordering: %#v", second)
	}
	if second.OperationID != "listPets" {
		t.Fatalf("expected operationId=listPets, got %q", second.OperationID)
	}
	if second.Summary != "List pets" {
		t.Fatalf("expected summary=List pets, got %q", second.Summary)
	}
	if second.Deprecated {
		t.Fatalf("expected get endpoint deprecated=false")
	}
	if len(second.RawJSON) == 0 {
		t.Fatalf("expected endpoint raw json")
	}
}
