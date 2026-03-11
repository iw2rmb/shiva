package request

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type CLIInputs struct {
	Path   []string
	Query  []string
	Header []string
	JSON   string
	Body   string
}

func ApplyCLIInputs(envelope Envelope, inputs CLIInputs) (Envelope, error) {
	if !hasCLIInputs(inputs) {
		return envelope, nil
	}
	if envelope.Kind != KindCall {
		return Envelope{}, invalid("call input flags require call mode")
	}

	pathParams, err := parseFlatAssignments(inputs.Path, "path")
	if err != nil {
		return Envelope{}, err
	}
	queryParams, err := parseRepeatedAssignments(inputs.Query, "query")
	if err != nil {
		return Envelope{}, err
	}
	headers, err := parseRepeatedAssignments(inputs.Header, "header")
	if err != nil {
		return Envelope{}, err
	}
	jsonBody, err := parseJSONInput(inputs.JSON)
	if err != nil {
		return Envelope{}, err
	}
	body, err := parseBodyInput(inputs.Body)
	if err != nil {
		return Envelope{}, err
	}

	envelope.PathParams = pathParams
	envelope.QueryParams = queryParams
	envelope.Headers = headers
	envelope.JSONBody = jsonBody
	envelope.Body = body

	normalized, err := NormalizeCallEnvelope(envelope, NormalizeCallOptions{
		DefaultTarget:    strings.TrimSpace(envelope.Target),
		AllowMissingKind: false,
	})
	if err != nil {
		return Envelope{}, err
	}
	return normalized, nil
}

func hasCLIInputs(inputs CLIInputs) bool {
	return len(inputs.Path) > 0 ||
		len(inputs.Query) > 0 ||
		len(inputs.Header) > 0 ||
		strings.TrimSpace(inputs.JSON) != "" ||
		strings.TrimSpace(inputs.Body) != ""
}

func parseFlatAssignments(values []string, field string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}

	result := make(map[string]string, len(values))
	for _, raw := range values {
		key, value, err := parseAssignment(raw, field)
		if err != nil {
			return nil, err
		}
		if _, exists := result[key]; exists {
			return nil, invalid("%s %q is duplicated", field, key)
		}
		result[key] = value
	}
	return result, nil
}

func parseRepeatedAssignments(values []string, field string) (map[string][]string, error) {
	if len(values) == 0 {
		return nil, nil
	}

	result := make(map[string][]string, len(values))
	for _, raw := range values {
		key, value, err := parseAssignment(raw, field)
		if err != nil {
			return nil, err
		}
		result[key] = append(result[key], value)
	}
	return result, nil
}

func parseAssignment(raw string, field string) (string, string, error) {
	key, value, found := strings.Cut(strings.TrimSpace(raw), "=")
	if !found {
		return "", "", invalid("%s must use key=value", field)
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", invalid("%s key must not be empty", field)
	}
	return key, value, nil
}

func parseJSONInput(raw string) (json.RawMessage, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, nil
	}

	content, err := readInlineOrFile(value, "json")
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil, invalid("json must not be empty")
	}
	if !json.Valid([]byte(trimmed)) {
		return nil, invalid("json must be valid json")
	}
	return json.RawMessage(trimmed), nil
}

func parseBodyInput(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if !strings.HasPrefix(value, "@") {
		return "", invalid("body must use @file")
	}
	content, err := readFileReference(value, "body")
	if err != nil {
		return "", err
	}
	return content, nil
}

func readInlineOrFile(value string, field string) (string, error) {
	if !strings.HasPrefix(value, "@") {
		return value, nil
	}
	return readFileReference(value, field)
}

func readFileReference(value string, field string) (string, error) {
	path := strings.TrimSpace(strings.TrimPrefix(value, "@"))
	if path == "" {
		return "", invalid("%s file path must not be empty", field)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s file %s: %w", field, path, err)
	}
	return string(content), nil
}
