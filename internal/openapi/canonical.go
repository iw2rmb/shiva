package openapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

var ErrCanonicalRootNotFound = errors.New("openapi canonical root not found")
var ErrCanonicalDocumentNotFound = errors.New("openapi canonical document not found")
var ErrReferencePointerNotFound = errors.New("openapi $ref pointer not found")

type CanonicalSpec struct {
	RootDocument string
	SpecJSON     []byte
	SpecYAML     string
	ETag         string
	SizeBytes    int64
	Endpoints    []Endpoint
}

type Endpoint struct {
	Method      string
	Path        string
	OperationID string
	Summary     string
	Deprecated  bool
	RawJSON     []byte
}

func BuildCanonicalSpec(result ResolutionResult) (CanonicalSpec, error) {
	rootPath, err := pickCanonicalRoot(result.CandidateFiles, result.Documents)
	if err != nil {
		return CanonicalSpec{}, err
	}

	builder := canonicalBuilder{
		rawDocuments:    result.Documents,
		parsedDocuments: make(map[string]any, len(result.Documents)),
	}

	rootDocument, err := builder.loadDocument(rootPath)
	if err != nil {
		return CanonicalSpec{}, err
	}

	canonicalDocument, err := builder.expandDocument(rootDocument, rootPath)
	if err != nil {
		return CanonicalSpec{}, err
	}

	specJSON, err := json.Marshal(canonicalDocument)
	if err != nil {
		return CanonicalSpec{}, fmt.Errorf("marshal canonical json: %w", err)
	}

	specYAML, err := renderCanonicalYAML(canonicalDocument)
	if err != nil {
		return CanonicalSpec{}, err
	}

	endpoints, err := extractEndpoints(canonicalDocument)
	if err != nil {
		return CanonicalSpec{}, err
	}

	hash := sha256.Sum256(specJSON)
	return CanonicalSpec{
		RootDocument: rootPath,
		SpecJSON:     specJSON,
		SpecYAML:     specYAML,
		ETag:         "\"" + hex.EncodeToString(hash[:]) + "\"",
		SizeBytes:    int64(len(specJSON) + len(specYAML)),
		Endpoints:    endpoints,
	}, nil
}

func pickCanonicalRoot(candidates []string, documents map[string][]byte) (string, error) {
	if len(candidates) == 0 {
		return "", ErrCanonicalRootNotFound
	}

	normalized := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		path := normalizeRepoPath(candidate)
		if path == "" {
			continue
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		normalized = append(normalized, path)
	}

	if len(normalized) == 0 {
		return "", ErrCanonicalRootNotFound
	}

	sort.Strings(normalized)
	for _, candidate := range normalized {
		if _, exists := documents[candidate]; exists {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("%w: no candidate document found in resolver output", ErrCanonicalDocumentNotFound)
}

type canonicalBuilder struct {
	rawDocuments    map[string][]byte
	parsedDocuments map[string]any
}

func (b *canonicalBuilder) loadDocument(filePath string) (any, error) {
	document, exists := b.parsedDocuments[filePath]
	if exists {
		return document, nil
	}

	content, exists := b.rawDocuments[filePath]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrCanonicalDocumentNotFound, filePath)
	}

	document, err := parseDocument(content)
	if err != nil {
		return nil, fmt.Errorf("%w: parse %q: %v", ErrInvalidOpenAPIDocument, filePath, err)
	}

	b.parsedDocuments[filePath] = document
	return document, nil
}

func (b *canonicalBuilder) expandDocument(document any, sourcePath string) (any, error) {
	switch typed := document.(type) {
	case map[string]any:
		return b.expandMap(typed, sourcePath)
	case []any:
		result := make([]any, len(typed))
		for i := range typed {
			expanded, err := b.expandDocument(typed[i], sourcePath)
			if err != nil {
				return nil, err
			}
			result[i] = expanded
		}
		return result, nil
	default:
		return typed, nil
	}
}

func (b *canonicalBuilder) expandMap(value map[string]any, sourcePath string) (map[string]any, error) {
	rawRef, hasRef := value["$ref"].(string)
	if hasRef {
		targetPath, pointer, isExternal, err := parseReference(sourcePath, rawRef)
		if err != nil {
			return nil, err
		}
		if isExternal {
			targetDocument, err := b.loadDocument(targetPath)
			if err != nil {
				return nil, err
			}

			targetValue, err := resolveJSONPointer(targetDocument, pointer)
			if err != nil {
				return nil, err
			}

			expandedTarget, err := b.expandDocument(targetValue, targetPath)
			if err != nil {
				return nil, err
			}

			if len(value) == 1 {
				targetMap, ok := expandedTarget.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("%w: %q in %q does not resolve to object", ErrInvalidReference, rawRef, sourcePath)
				}
				return targetMap, nil
			}

			targetMap, ok := expandedTarget.(map[string]any)
			if !ok {
				return nil, fmt.Errorf(
					"%w: %q in %q has sibling fields but target is not an object",
					ErrInvalidReference,
					rawRef,
					sourcePath,
				)
			}

			merged := cloneMap(targetMap)
			for key, rawNested := range value {
				if key == "$ref" {
					continue
				}
				expandedNested, err := b.expandDocument(rawNested, sourcePath)
				if err != nil {
					return nil, err
				}
				merged[key] = expandedNested
			}
			return merged, nil
		}
	}

	expanded := make(map[string]any, len(value))
	for key, rawNested := range value {
		expandedNested, err := b.expandDocument(rawNested, sourcePath)
		if err != nil {
			return nil, err
		}
		expanded[key] = expandedNested
	}
	return expanded, nil
}

func parseReference(sourcePath string, rawRef string) (targetPath string, pointer string, isExternal bool, err error) {
	ref := strings.TrimSpace(rawRef)
	if ref == "" {
		return "", "", false, fmt.Errorf("%w: empty $ref in %q", ErrInvalidReference, sourcePath)
	}

	parsed, err := url.Parse(ref)
	if err != nil {
		return "", "", false, fmt.Errorf("%w: %q in %q is not a valid URI: %v", ErrInvalidReference, rawRef, sourcePath, err)
	}
	if parsed.Scheme != "" || parsed.Host != "" {
		return "", "", false, fmt.Errorf(
			"%w: external reference %q in %q is not supported",
			ErrInvalidReference,
			rawRef,
			sourcePath,
		)
	}

	if parsed.Fragment != "" {
		pointer, err = url.PathUnescape(parsed.Fragment)
		if err != nil {
			return "", "", false, fmt.Errorf("%w: invalid fragment in %q: %v", ErrInvalidReference, rawRef, err)
		}
		if !strings.HasPrefix(pointer, "/") {
			return "", "", false, fmt.Errorf(
				"%w: anchor fragment %q in %q is not supported",
				ErrInvalidReference,
				rawRef,
				sourcePath,
			)
		}
	}

	if parsed.Path == "" {
		return sourcePath, pointer, false, nil
	}

	resolved, err := resolveLocalRefTarget(sourcePath, ref)
	if err != nil {
		return "", "", false, err
	}
	if resolved == "" {
		return sourcePath, pointer, false, nil
	}

	return resolved, pointer, true, nil
}

func resolveJSONPointer(document any, pointer string) (any, error) {
	if pointer == "" {
		return document, nil
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, fmt.Errorf("%w: %q must start with /", ErrInvalidReference, pointer)
	}

	current := document
	parts := strings.Split(pointer[1:], "/")
	for _, rawPart := range parts {
		token := decodePointerToken(rawPart)
		switch typed := current.(type) {
		case map[string]any:
			next, exists := typed[token]
			if !exists {
				return nil, fmt.Errorf("%w: %q", ErrReferencePointerNotFound, pointer)
			}
			current = next
		case []any:
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(typed) {
				return nil, fmt.Errorf("%w: %q", ErrReferencePointerNotFound, pointer)
			}
			current = typed[index]
		default:
			return nil, fmt.Errorf("%w: %q", ErrReferencePointerNotFound, pointer)
		}
	}

	return current, nil
}

func decodePointerToken(value string) string {
	token := strings.ReplaceAll(value, "~1", "/")
	token = strings.ReplaceAll(token, "~0", "~")
	return token
}

func cloneMap(source map[string]any) map[string]any {
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = cloneValue(value)
	}
	return cloned
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		cloned := make([]any, len(typed))
		for i := range typed {
			cloned[i] = cloneValue(typed[i])
		}
		return cloned
	default:
		return typed
	}
}

func renderCanonicalYAML(document any) (string, error) {
	root, err := toYAMLNode(document)
	if err != nil {
		return "", err
	}

	doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}
	buffer := bytes.Buffer{}
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		return "", fmt.Errorf("encode canonical yaml: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return "", fmt.Errorf("close yaml encoder: %w", err)
	}

	return buffer.String(), nil
}

func toYAMLNode(value any) (*yaml.Node, error) {
	switch typed := value.(type) {
	case map[string]any:
		node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		for _, key := range keys {
			keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
			valueNode, err := toYAMLNode(typed[key])
			if err != nil {
				return nil, err
			}
			node.Content = append(node.Content, keyNode, valueNode)
		}
		return node, nil
	case []any:
		node := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, item := range typed {
			itemNode, err := toYAMLNode(item)
			if err != nil {
				return nil, err
			}
			node.Content = append(node.Content, itemNode)
		}
		return node, nil
	case string:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: typed}, nil
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(typed)}, nil
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	case int:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(typed)}, nil
	case int8:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(int64(typed), 10)}, nil
	case int16:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(int64(typed), 10)}, nil
	case int32:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(int64(typed), 10)}, nil
	case int64:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(typed, 10)}, nil
	case uint:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatUint(uint64(typed), 10)}, nil
	case uint8:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatUint(uint64(typed), 10)}, nil
	case uint16:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatUint(uint64(typed), 10)}, nil
	case uint32:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatUint(uint64(typed), 10)}, nil
	case uint64:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatUint(typed, 10)}, nil
	case float32:
		return &yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!float",
			Value: strconv.FormatFloat(float64(typed), 'g', -1, 32),
		}, nil
	case float64:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: strconv.FormatFloat(typed, 'g', -1, 64)}, nil
	default:
		return nil, fmt.Errorf("unsupported yaml node type %T", typed)
	}
}

func extractEndpoints(document any) ([]Endpoint, error) {
	root, ok := document.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: canonical root is not an object", ErrInvalidOpenAPIDocument)
	}

	rawPaths, exists := root["paths"]
	if !exists {
		return []Endpoint{}, nil
	}

	paths, ok := rawPaths.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: paths is not an object", ErrInvalidOpenAPIDocument)
	}

	methodSet := map[string]struct{}{
		"get":     {},
		"put":     {},
		"post":    {},
		"delete":  {},
		"options": {},
		"head":    {},
		"patch":   {},
		"trace":   {},
	}

	pathKeys := make([]string, 0, len(paths))
	for pathKey := range paths {
		pathKeys = append(pathKeys, pathKey)
	}
	sort.Strings(pathKeys)

	endpoints := make([]Endpoint, 0)
	for _, pathKey := range pathKeys {
		rawPathItem := paths[pathKey]
		pathItem, ok := rawPathItem.(map[string]any)
		if !ok {
			continue
		}

		methodKeys := make([]string, 0, len(pathItem))
		for method := range pathItem {
			methodKeys = append(methodKeys, method)
		}
		sort.Strings(methodKeys)

		for _, method := range methodKeys {
			methodLower := strings.ToLower(method)
			if _, exists := methodSet[methodLower]; !exists {
				continue
			}

			rawOperation := pathItem[method]
			operation, ok := rawOperation.(map[string]any)
			if !ok {
				continue
			}

			rawJSON, err := json.Marshal(operation)
			if err != nil {
				return nil, fmt.Errorf("marshal endpoint %s %s: %w", methodLower, pathKey, err)
			}

			endpoint := Endpoint{
				Method:      methodLower,
				Path:        pathKey,
				RawJSON:     rawJSON,
				Summary:     optionalString(operation["summary"]),
				Deprecated:  optionalBool(operation["deprecated"]),
				OperationID: optionalString(operation["operationId"]),
			}
			endpoints = append(endpoints, endpoint)
		}
	}

	sort.SliceStable(endpoints, func(i, j int) bool {
		if endpoints[i].Method == endpoints[j].Method {
			return endpoints[i].Path < endpoints[j].Path
		}
		return endpoints[i].Method < endpoints[j].Method
	})

	return endpoints, nil
}

func optionalString(value any) string {
	stringValue, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(stringValue)
}

func optionalBool(value any) bool {
	boolValue, ok := value.(bool)
	if !ok {
		return false
	}
	return boolValue
}
