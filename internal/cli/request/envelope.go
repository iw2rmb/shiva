package request

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/iw2rmb/shiva/internal/repoid"
)

type Kind string

const (
	KindSpec      Kind = "spec"
	KindOperation Kind = "operation"
	KindCall      Kind = "call"

	DefaultShivaTarget = "shiva"
)

type Envelope struct {
	Kind        Kind                `json:"kind,omitempty"`
	Namespace   string              `json:"namespace"`
	Repo        string              `json:"repo"`
	API         string              `json:"api,omitempty"`
	RevisionID  int64               `json:"revision_id,omitempty"`
	SHA         string              `json:"sha,omitempty"`
	Target      string              `json:"target,omitempty"`
	OperationID string              `json:"operation_id,omitempty"`
	Method      string              `json:"method,omitempty"`
	Path        string              `json:"path,omitempty"`
	PathParams  map[string]string   `json:"path_params,omitempty"`
	QueryParams map[string][]string `json:"query_params,omitempty"`
	Headers     map[string][]string `json:"headers,omitempty"`
	JSONBody    json.RawMessage     `json:"json,omitempty"`
	Body        string              `json:"body,omitempty"`
	DryRun      bool                `json:"dry_run,omitempty"`
}

func (e Envelope) RepoPath() string {
	return repoid.Identity{Namespace: e.Namespace, Repo: e.Repo}.Path()
}

type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	if e == nil || strings.TrimSpace(e.Message) == "" {
		return "invalid request"
	}
	return e.Message
}

type NormalizeCallOptions struct {
	DefaultTarget    string
	AllowMissingKind bool
}

type NormalizeOptions struct {
	DefaultKind      Kind
	DefaultTarget    string
	AllowMissingKind bool
}

func NormalizeEnvelope(input Envelope, options NormalizeOptions) (Envelope, error) {
	namespace, repo, api, revisionID, sha, err := NormalizeSnapshotSelector(
		input.Namespace,
		input.Repo,
		input.API,
		input.RevisionID,
		input.SHA,
	)
	if err != nil {
		return Envelope{}, err
	}

	kind, err := normalizeKind(input.Kind, options.DefaultKind, options.AllowMissingKind)
	if err != nil {
		return Envelope{}, err
	}

	target := strings.TrimSpace(input.Target)
	if target == "" {
		target = strings.TrimSpace(options.DefaultTarget)
	}

	pathParams, queryParams, headers, jsonBody, body, err := normalizeRequestInputs(input)
	if err != nil {
		return Envelope{}, err
	}

	normalized := Envelope{
		Kind:        kind,
		Namespace:   namespace,
		Repo:        repo,
		API:         api,
		RevisionID:  revisionID,
		SHA:         sha,
		PathParams:  pathParams,
		QueryParams: queryParams,
		Headers:     headers,
		JSONBody:    jsonBody,
		Body:        body,
		DryRun:      input.DryRun,
	}

	switch kind {
	case KindSpec:
		if hasOperationSelectorInput(input) {
			return Envelope{}, invalid("operation selector is not supported for kind %q", KindSpec)
		}
		if hasCallOnlyInput(input) {
			return Envelope{}, invalid("call inputs are not supported for kind %q", KindSpec)
		}
	case KindOperation:
		operationID, method, path, err := NormalizeOperationSelector(input.OperationID, input.Method, input.Path)
		if err != nil {
			return Envelope{}, err
		}
		if hasCallOnlyInput(input) {
			return Envelope{}, invalid("call inputs are not supported for kind %q", KindOperation)
		}
		normalized.OperationID = operationID
		normalized.Method = method
		normalized.Path = path
	case KindCall:
		operationID, method, path, err := NormalizeOperationSelector(input.OperationID, input.Method, input.Path)
		if err != nil {
			return Envelope{}, err
		}
		normalized.Target = target
		normalized.OperationID = operationID
		normalized.Method = method
		normalized.Path = path
	default:
		return Envelope{}, invalid("unsupported kind %q", kind)
	}

	return normalized, nil
}

func NormalizeCallEnvelope(input Envelope, options NormalizeCallOptions) (Envelope, error) {
	return NormalizeEnvelope(input, NormalizeOptions{
		DefaultKind:      KindCall,
		DefaultTarget:    options.DefaultTarget,
		AllowMissingKind: options.AllowMissingKind,
	})
}

func NormalizeResolvedCallEnvelope(input Envelope, defaultTarget string) (Envelope, error) {
	kind, err := normalizeKind(input.Kind, KindCall, true)
	if err != nil {
		return Envelope{}, err
	}
	if kind != KindCall {
		return Envelope{}, invalid("kind must be %q", KindCall)
	}

	identity, err := repoid.Normalize(input.Namespace, input.Repo)
	if err != nil {
		return Envelope{}, invalid(err.Error())
	}
	api := strings.TrimSpace(input.API)
	if api == "" {
		return Envelope{}, invalid("api must not be empty")
	}
	if input.RevisionID < 1 {
		return Envelope{}, invalid("revision_id must be a positive integer")
	}

	sha := strings.TrimSpace(input.SHA)
	if sha != "" && !IsShortSHA(sha) {
		return Envelope{}, invalid("sha must be exactly 8 lowercase hex characters")
	}

	target := strings.TrimSpace(input.Target)
	if target == "" {
		target = strings.TrimSpace(defaultTarget)
	}

	_, method, path, err := NormalizeOperationSelector("", input.Method, input.Path)
	if err != nil {
		return Envelope{}, err
	}

	pathParams, queryParams, headers, jsonBody, body, err := normalizeRequestInputs(input)
	if err != nil {
		return Envelope{}, err
	}

	return Envelope{
		Kind:        kind,
		Namespace:   identity.Namespace,
		Repo:        identity.Repo,
		API:         api,
		RevisionID:  input.RevisionID,
		SHA:         sha,
		Target:      target,
		OperationID: strings.TrimSpace(input.OperationID),
		Method:      method,
		Path:        path,
		PathParams:  pathParams,
		QueryParams: queryParams,
		Headers:     headers,
		JSONBody:    jsonBody,
		Body:        body,
		DryRun:      input.DryRun,
	}, nil
}

func NormalizeSnapshotSelector(
	namespace string,
	repo string,
	api string,
	revisionID int64,
	sha string,
) (string, string, string, int64, string, error) {
	identity, err := repoid.Normalize(namespace, repo)
	if err != nil {
		return "", "", "", 0, "", invalid(err.Error())
	}

	api = strings.TrimSpace(api)
	if revisionID < 0 {
		return "", "", "", 0, "", invalid("revision_id must be a positive integer")
	}
	if revisionID == 0 && sha == "" {
		return identity.Namespace, identity.Repo, api, 0, "", nil
	}
	if revisionID == 0 && strings.TrimSpace(sha) == "" && sha != "" {
		return "", "", "", 0, "", invalid("sha must not be empty")
	}
	if revisionID > 0 && strings.TrimSpace(sha) != "" {
		return "", "", "", 0, "", invalid("revision_id and sha are mutually exclusive")
	}

	sha = strings.TrimSpace(sha)
	if sha != "" && !IsShortSHA(sha) {
		return "", "", "", 0, "", invalid("sha must be exactly 8 lowercase hex characters")
	}
	if revisionID == 0 && sha == "" {
		return identity.Namespace, identity.Repo, api, 0, "", nil
	}
	return identity.Namespace, identity.Repo, api, revisionID, sha, nil
}

func NormalizeOperationSelector(operationID string, method string, path string) (string, string, string, error) {
	operationIDPresent := strings.TrimSpace(operationID) != ""
	methodPresent := strings.TrimSpace(method) != ""
	pathPresent := strings.TrimSpace(path) != ""

	if operationIDPresent {
		if methodPresent || pathPresent {
			return "", "", "", invalid("operation_id is mutually exclusive with method and path")
		}
		return strings.TrimSpace(operationID), "", "", nil
	}
	if !methodPresent && !pathPresent {
		return "", "", "", invalid("either operation_id or method and path are required")
	}
	if !methodPresent || !pathPresent {
		return "", "", "", invalid("method and path must be provided together")
	}

	normalizedMethod := NormalizeHTTPMethod(method)
	if normalizedMethod == "" {
		return "", "", "", invalid("method must be one of get, post, put, patch, delete, head, options, trace")
	}

	normalizedPath := NormalizeLookupPath(path)
	if normalizedPath == "" {
		return "", "", "", invalid("path must not be empty")
	}

	return "", normalizedMethod, normalizedPath, nil
}

func NormalizeHTTPMethod(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "get", "post", "put", "patch", "delete", "head", "options", "trace":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func NormalizeLookupPath(value string) string {
	path := strings.TrimSpace(value)
	if path == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	segments := strings.Split(path, "/")
	for index := range segments {
		segment := segments[index]
		if len(segment) > 1 && strings.HasPrefix(segment, ":") {
			segments[index] = "{" + segment[1:] + "}"
		}
	}

	return strings.Join(segments, "/")
}

func IsShortSHA(value string) bool {
	if len(value) != 8 {
		return false
	}
	for _, character := range value {
		switch {
		case character >= '0' && character <= '9':
		case character >= 'a' && character <= 'f':
		default:
			return false
		}
	}
	return true
}

func normalizeFlatMap(input map[string]string, field string) (map[string]string, error) {
	if len(input) == 0 {
		return nil, nil
	}

	output := make(map[string]string, len(input))
	for key, value := range input {
		normalizedKey := strings.TrimSpace(key)
		if normalizedKey == "" {
			return nil, invalid("%s keys must not be empty", field)
		}
		output[normalizedKey] = value
	}
	return output, nil
}

func normalizeListMap(input map[string][]string, field string) (map[string][]string, error) {
	if len(input) == 0 {
		return nil, nil
	}

	output := make(map[string][]string, len(input))
	for key, values := range input {
		normalizedKey := strings.TrimSpace(key)
		if normalizedKey == "" {
			return nil, invalid("%s keys must not be empty", field)
		}
		copied := make([]string, len(values))
		copy(copied, values)
		output[normalizedKey] = copied
	}
	return output, nil
}

func normalizeRequestInputs(input Envelope) (
	map[string]string,
	map[string][]string,
	map[string][]string,
	json.RawMessage,
	string,
	error,
) {
	pathParams, err := normalizeFlatMap(input.PathParams, "path_params")
	if err != nil {
		return nil, nil, nil, nil, "", err
	}
	queryParams, err := normalizeListMap(input.QueryParams, "query_params")
	if err != nil {
		return nil, nil, nil, nil, "", err
	}
	headers, err := normalizeListMap(input.Headers, "headers")
	if err != nil {
		return nil, nil, nil, nil, "", err
	}
	jsonBody, err := normalizeJSONBody(input.JSONBody)
	if err != nil {
		return nil, nil, nil, nil, "", err
	}
	body := input.Body
	if len(jsonBody) > 0 && strings.TrimSpace(body) != "" {
		return nil, nil, nil, nil, "", invalid("json and body are mutually exclusive")
	}
	return pathParams, queryParams, headers, jsonBody, body, nil
}

func normalizeKind(kind Kind, defaultKind Kind, allowMissingKind bool) (Kind, error) {
	switch {
	case kind == "":
		if !allowMissingKind {
			return "", invalid("kind must be %q", defaultKind)
		}
		return defaultKind, nil
	case kind == KindSpec || kind == KindOperation || kind == KindCall:
		return kind, nil
	default:
		return "", invalid("unsupported kind %q", kind)
	}
}

func hasOperationSelectorInput(input Envelope) bool {
	return strings.TrimSpace(input.OperationID) != "" ||
		strings.TrimSpace(input.Method) != "" ||
		strings.TrimSpace(input.Path) != ""
}

func hasCallOnlyInput(input Envelope) bool {
	return strings.TrimSpace(input.Target) != "" ||
		len(input.PathParams) > 0 ||
		len(input.QueryParams) > 0 ||
		len(input.Headers) > 0 ||
		len(input.JSONBody) > 0 ||
		strings.TrimSpace(input.Body) != "" ||
		input.DryRun
}

func normalizeJSONBody(input json.RawMessage) (json.RawMessage, error) {
	if len(input) == 0 {
		return nil, nil
	}

	trimmed := strings.TrimSpace(string(input))
	if trimmed == "" {
		return nil, nil
	}
	if !json.Valid([]byte(trimmed)) {
		return nil, invalid("json must be valid json")
	}
	return json.RawMessage(trimmed), nil
}

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}
