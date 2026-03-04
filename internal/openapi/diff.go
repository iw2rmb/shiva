package openapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const SpecChangesVersion = 1

type EndpointSnapshot struct {
	Method  string
	Path    string
	RawJSON []byte
}

type EndpointRef struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

type FieldChanges struct {
	Added   []string `json:"added"`
	Removed []string `json:"removed"`
	Changed []string `json:"changed"`
}

type EndpointChange struct {
	Method      string       `json:"method"`
	Path        string       `json:"path"`
	ChangeTypes []string     `json:"change_types"`
	Parameters  FieldChanges `json:"parameters"`
	Schemas     FieldChanges `json:"schemas"`
}

type EndpointChanges struct {
	Added   []EndpointRef    `json:"added"`
	Removed []EndpointRef    `json:"removed"`
	Changed []EndpointChange `json:"changed"`
}

type SpecChangesSummary struct {
	AddedEndpoints   int `json:"added_endpoints"`
	RemovedEndpoints int `json:"removed_endpoints"`
	ChangedEndpoints int `json:"changed_endpoints"`
	ParameterChanges int `json:"parameter_changes"`
	SchemaChanges    int `json:"schema_changes"`
}

type SpecChanges struct {
	Version   int                `json:"version"`
	Endpoints EndpointChanges    `json:"endpoints"`
	Summary   SpecChangesSummary `json:"summary"`
}

func ComputeSemanticDiff(previous []EndpointSnapshot, current []EndpointSnapshot) (SpecChanges, error) {
	previousNormalized, err := normalizeEndpointSnapshots(previous)
	if err != nil {
		return SpecChanges{}, fmt.Errorf("normalize previous endpoints: %w", err)
	}

	currentNormalized, err := normalizeEndpointSnapshots(current)
	if err != nil {
		return SpecChanges{}, fmt.Errorf("normalize current endpoints: %w", err)
	}

	result := SpecChanges{
		Version: SpecChangesVersion,
		Endpoints: EndpointChanges{
			Added:   make([]EndpointRef, 0),
			Removed: make([]EndpointRef, 0),
			Changed: make([]EndpointChange, 0),
		},
	}

	for key, endpoint := range currentNormalized {
		if _, exists := previousNormalized[key]; exists {
			continue
		}
		result.Endpoints.Added = append(result.Endpoints.Added, endpoint.ref)
	}

	for key, endpoint := range previousNormalized {
		if _, exists := currentNormalized[key]; exists {
			continue
		}
		result.Endpoints.Removed = append(result.Endpoints.Removed, endpoint.ref)
	}

	for key, previousEndpoint := range previousNormalized {
		currentEndpoint, exists := currentNormalized[key]
		if !exists {
			continue
		}
		if bytes.Equal(previousEndpoint.rawJSON, currentEndpoint.rawJSON) {
			continue
		}

		previousParameters, err := extractParameterFingerprints(previousEndpoint.operation)
		if err != nil {
			return SpecChanges{}, fmt.Errorf(
				"extract previous parameters for %s %s: %w",
				previousEndpoint.ref.Method,
				previousEndpoint.ref.Path,
				err,
			)
		}
		currentParameters, err := extractParameterFingerprints(currentEndpoint.operation)
		if err != nil {
			return SpecChanges{}, fmt.Errorf(
				"extract current parameters for %s %s: %w",
				currentEndpoint.ref.Method,
				currentEndpoint.ref.Path,
				err,
			)
		}
		parameterChanges := diffJSONFingerprintMaps(previousParameters, currentParameters)

		previousSchemas, err := extractSchemaFingerprints(previousEndpoint.operation)
		if err != nil {
			return SpecChanges{}, fmt.Errorf(
				"extract previous schemas for %s %s: %w",
				previousEndpoint.ref.Method,
				previousEndpoint.ref.Path,
				err,
			)
		}
		currentSchemas, err := extractSchemaFingerprints(currentEndpoint.operation)
		if err != nil {
			return SpecChanges{}, fmt.Errorf(
				"extract current schemas for %s %s: %w",
				currentEndpoint.ref.Method,
				currentEndpoint.ref.Path,
				err,
			)
		}
		schemaChanges := diffJSONFingerprintMaps(previousSchemas, currentSchemas)

		changeTypes := make([]string, 0, 3)
		if hasFieldChanges(parameterChanges) {
			changeTypes = append(changeTypes, "parameters")
		}
		if hasFieldChanges(schemaChanges) {
			changeTypes = append(changeTypes, "schemas")
		}
		if len(changeTypes) == 0 {
			changeTypes = append(changeTypes, "operation")
		}

		result.Endpoints.Changed = append(result.Endpoints.Changed, EndpointChange{
			Method:      currentEndpoint.ref.Method,
			Path:        currentEndpoint.ref.Path,
			ChangeTypes: changeTypes,
			Parameters:  parameterChanges,
			Schemas:     schemaChanges,
		})
	}

	sortEndpointRefs(result.Endpoints.Added)
	sortEndpointRefs(result.Endpoints.Removed)
	sortEndpointChanges(result.Endpoints.Changed)

	result.Summary = SpecChangesSummary{
		AddedEndpoints:   len(result.Endpoints.Added),
		RemovedEndpoints: len(result.Endpoints.Removed),
		ChangedEndpoints: len(result.Endpoints.Changed),
	}

	for _, changedEndpoint := range result.Endpoints.Changed {
		result.Summary.ParameterChanges += len(changedEndpoint.Parameters.Added) +
			len(changedEndpoint.Parameters.Removed) +
			len(changedEndpoint.Parameters.Changed)
		result.Summary.SchemaChanges += len(changedEndpoint.Schemas.Added) +
			len(changedEndpoint.Schemas.Removed) +
			len(changedEndpoint.Schemas.Changed)
	}

	return result, nil
}

type normalizedEndpointSnapshot struct {
	ref       EndpointRef
	rawJSON   []byte
	operation map[string]any
}

func normalizeEndpointSnapshots(endpoints []EndpointSnapshot) (map[string]normalizedEndpointSnapshot, error) {
	normalized := make(map[string]normalizedEndpointSnapshot, len(endpoints))
	for _, endpoint := range endpoints {
		method := strings.ToLower(strings.TrimSpace(endpoint.Method))
		path := strings.TrimSpace(endpoint.Path)
		if method == "" {
			return nil, errors.New("endpoint method must not be empty")
		}
		if path == "" {
			return nil, errors.New("endpoint path must not be empty")
		}
		if len(endpoint.RawJSON) == 0 {
			return nil, fmt.Errorf("endpoint raw_json must not be empty for %s %s", method, path)
		}

		var operationProbe any
		if err := json.Unmarshal(endpoint.RawJSON, &operationProbe); err != nil {
			return nil, fmt.Errorf("endpoint raw_json is invalid for %s %s: %w", method, path, err)
		}

		operation, ok := operationProbe.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("endpoint raw_json must be an object for %s %s", method, path)
		}

		canonicalRawJSON, err := json.Marshal(operation)
		if err != nil {
			return nil, fmt.Errorf("marshal endpoint raw_json for %s %s: %w", method, path, err)
		}

		key := endpointKey(method, path)
		if _, exists := normalized[key]; exists {
			return nil, fmt.Errorf("duplicate endpoint key: method=%s path=%s", method, path)
		}

		normalized[key] = normalizedEndpointSnapshot{
			ref: EndpointRef{
				Method: method,
				Path:   path,
			},
			rawJSON:   canonicalRawJSON,
			operation: operation,
		}
	}
	return normalized, nil
}

func endpointKey(method string, path string) string {
	return method + "\x00" + path
}

func extractParameterFingerprints(operation map[string]any) (map[string][]byte, error) {
	result := make(map[string][]byte)
	for _, parameter := range extractOperationParameters(operation) {
		fingerprint, err := json.Marshal(parameter.value)
		if err != nil {
			return nil, fmt.Errorf("marshal parameter %q: %w", parameter.key, err)
		}
		result[parameter.key] = fingerprint
	}
	return result, nil
}

func extractSchemaFingerprints(operation map[string]any) (map[string][]byte, error) {
	result := make(map[string][]byte)

	for _, parameter := range extractOperationParameters(operation) {
		if rawSchema, exists := parameter.value["schema"]; exists {
			fingerprint, err := json.Marshal(rawSchema)
			if err != nil {
				return nil, fmt.Errorf("marshal %s.schema: %w", parameter.key, err)
			}
			result["parameters."+parameter.key+".schema"] = fingerprint
		}

		if err := collectContentSchemas("parameters."+parameter.key, parameter.value, result); err != nil {
			return nil, err
		}
	}

	requestBody, _ := operation["requestBody"].(map[string]any)
	if requestBody != nil {
		if err := collectContentSchemas("request_body", requestBody, result); err != nil {
			return nil, err
		}
	}

	responses, _ := operation["responses"].(map[string]any)
	if responses != nil {
		statusCodes := make([]string, 0, len(responses))
		for statusCode := range responses {
			statusCodes = append(statusCodes, statusCode)
		}
		sort.Strings(statusCodes)

		for _, statusCode := range statusCodes {
			response, _ := responses[statusCode].(map[string]any)
			if response == nil {
				continue
			}
			prefix := "responses." + statusCode
			if err := collectContentSchemas(prefix, response, result); err != nil {
				return nil, err
			}
		}
	}

	return result, nil
}

func collectContentSchemas(prefix string, value map[string]any, output map[string][]byte) error {
	content, _ := value["content"].(map[string]any)
	if content == nil {
		return nil
	}

	mediaTypes := make([]string, 0, len(content))
	for mediaType := range content {
		mediaTypes = append(mediaTypes, mediaType)
	}
	sort.Strings(mediaTypes)

	for _, mediaType := range mediaTypes {
		mediaValue, _ := content[mediaType].(map[string]any)
		if mediaValue == nil {
			continue
		}

		rawSchema, exists := mediaValue["schema"]
		if !exists {
			continue
		}

		fingerprint, err := json.Marshal(rawSchema)
		if err != nil {
			return fmt.Errorf("marshal %s.content.%s.schema: %w", prefix, mediaType, err)
		}
		output[prefix+".content."+mediaType+".schema"] = fingerprint
	}

	return nil
}

type operationParameter struct {
	key   string
	value map[string]any
}

func extractOperationParameters(operation map[string]any) []operationParameter {
	rawParameters, _ := operation["parameters"].([]any)
	if len(rawParameters) == 0 {
		return nil
	}

	parameters := make([]operationParameter, 0, len(rawParameters))
	for index, rawParameter := range rawParameters {
		parameter, _ := rawParameter.(map[string]any)
		if parameter == nil {
			continue
		}

		parameters = append(parameters, operationParameter{
			key:   buildParameterKey(parameter, index),
			value: parameter,
		})
	}

	sort.SliceStable(parameters, func(i, j int) bool {
		return parameters[i].key < parameters[j].key
	})
	return parameters
}

func buildParameterKey(parameter map[string]any, index int) string {
	if rawReference, ok := parameter["$ref"].(string); ok {
		reference := strings.TrimSpace(rawReference)
		if reference != "" {
			return "$ref:" + reference
		}
	}

	name := strings.TrimSpace(stringValue(parameter["name"]))
	location := strings.ToLower(strings.TrimSpace(stringValue(parameter["in"])))
	if name != "" || location != "" {
		return location + ":" + name
	}

	return fmt.Sprintf("index:%06d", index)
}

func stringValue(value any) string {
	stringValue, _ := value.(string)
	return stringValue
}

func diffJSONFingerprintMaps(previous map[string][]byte, current map[string][]byte) FieldChanges {
	changes := FieldChanges{
		Added:   make([]string, 0),
		Removed: make([]string, 0),
		Changed: make([]string, 0),
	}

	for key, currentFingerprint := range current {
		previousFingerprint, exists := previous[key]
		if !exists {
			changes.Added = append(changes.Added, key)
			continue
		}
		if bytes.Equal(previousFingerprint, currentFingerprint) {
			continue
		}
		changes.Changed = append(changes.Changed, key)
	}

	for key := range previous {
		if _, exists := current[key]; exists {
			continue
		}
		changes.Removed = append(changes.Removed, key)
	}

	sort.Strings(changes.Added)
	sort.Strings(changes.Removed)
	sort.Strings(changes.Changed)
	return changes
}

func hasFieldChanges(changes FieldChanges) bool {
	return len(changes.Added) > 0 || len(changes.Removed) > 0 || len(changes.Changed) > 0
}

func sortEndpointRefs(endpoints []EndpointRef) {
	sort.SliceStable(endpoints, func(i, j int) bool {
		if endpoints[i].Method == endpoints[j].Method {
			return endpoints[i].Path < endpoints[j].Path
		}
		return endpoints[i].Method < endpoints[j].Method
	})
}

func sortEndpointChanges(changes []EndpointChange) {
	for i := range changes {
		sort.Strings(changes[i].ChangeTypes)
		sort.Strings(changes[i].Parameters.Added)
		sort.Strings(changes[i].Parameters.Removed)
		sort.Strings(changes[i].Parameters.Changed)
		sort.Strings(changes[i].Schemas.Added)
		sort.Strings(changes[i].Schemas.Removed)
		sort.Strings(changes[i].Schemas.Changed)
	}
	sort.SliceStable(changes, func(i, j int) bool {
		if changes[i].Method == changes[j].Method {
			return changes[i].Path < changes[j].Path
		}
		return changes[i].Method < changes[j].Method
	})
}
