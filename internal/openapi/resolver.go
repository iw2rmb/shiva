package openapi

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/iw2rmb/shiva/internal/gitlab"
	"gopkg.in/yaml.v3"
)

const DefaultMaxFetches = 128

var defaultIncludeGlobs = []string{
	"**/openapi*.{yaml,yml,json}",
	"**/swagger*.{yaml,yml,json}",
	"**/api/**/*.yaml",
}

var ErrInvalidOpenAPIDocument = errors.New("invalid openapi document")
var ErrReferenceCycle = errors.New("openapi $ref cycle detected")
var ErrFetchLimitExceeded = errors.New("openapi fetch limit exceeded")
var ErrInvalidReference = errors.New("invalid openapi $ref")

type GitLabClient interface {
	CompareChangedPaths(ctx context.Context, projectID int64, fromSHA, toSHA string) ([]gitlab.ChangedPath, error)
	GetFileContent(ctx context.Context, projectID int64, filePath, ref string) ([]byte, error)
}

type ResolverConfig struct {
	IncludeGlobs []string
	MaxFetches   int
}

type Resolver struct {
	includeGlobs []string
	maxFetches   int
}

type ResolutionResult struct {
	OpenAPIChanged bool
	CandidateFiles []string
	Documents      map[string][]byte
}

func DefaultIncludeGlobs() []string {
	globs := make([]string, len(defaultIncludeGlobs))
	copy(globs, defaultIncludeGlobs)
	return globs
}

func NewResolver(cfg ResolverConfig) (*Resolver, error) {
	globs := cfg.IncludeGlobs
	if len(globs) == 0 {
		globs = DefaultIncludeGlobs()
	}

	normalizedGlobs := make([]string, 0, len(globs))
	for _, glob := range globs {
		trimmed := strings.TrimSpace(strings.TrimPrefix(glob, "/"))
		if trimmed == "" {
			return nil, errors.New("openapi include glob must not be empty")
		}
		normalizedGlobs = append(normalizedGlobs, trimmed)
	}

	maxFetches := cfg.MaxFetches
	if maxFetches <= 0 {
		maxFetches = DefaultMaxFetches
	}

	return &Resolver{
		includeGlobs: normalizedGlobs,
		maxFetches:   maxFetches,
	}, nil
}

func (r *Resolver) ResolveChangedOpenAPI(
	ctx context.Context,
	client GitLabClient,
	projectID int64,
	fromSHA string,
	toSHA string,
) (ResolutionResult, error) {
	if client == nil {
		return ResolutionResult{}, errors.New("gitlab client is required")
	}
	if projectID < 1 {
		return ResolutionResult{}, errors.New("project id must be positive")
	}
	if strings.TrimSpace(fromSHA) == "" {
		return ResolutionResult{}, errors.New("from sha must not be empty")
	}
	if strings.TrimSpace(toSHA) == "" {
		return ResolutionResult{}, errors.New("to sha must not be empty")
	}

	changedPaths, err := client.CompareChangedPaths(ctx, projectID, fromSHA, toSHA)
	if err != nil {
		return ResolutionResult{}, fmt.Errorf("load changed paths: %w", err)
	}

	roots := make([]string, 0)
	rootSet := make(map[string]struct{})
	rootDocuments := make(map[string][]byte)
	hasDeletedCandidate := false

	for _, changedPath := range changedPaths {
		candidatePath := detectCandidatePath(changedPath)
		if candidatePath == "" {
			continue
		}

		matches, err := r.matchesIncludeGlob(candidatePath)
		if err != nil {
			return ResolutionResult{}, fmt.Errorf("match include globs for %q: %w", candidatePath, err)
		}
		if !matches {
			continue
		}

		if changedPath.DeletedFile {
			hasDeletedCandidate = true
			continue
		}

		content, err := client.GetFileContent(ctx, projectID, candidatePath, toSHA)
		if err != nil {
			return ResolutionResult{}, fmt.Errorf("fetch candidate %q: %w", candidatePath, err)
		}

		parsed, err := parseDocument(content)
		if err != nil {
			return ResolutionResult{}, fmt.Errorf("%w: parse %q: %v", ErrInvalidOpenAPIDocument, candidatePath, err)
		}
		if !hasTopLevelOpenAPIOrSwagger(parsed) {
			return ResolutionResult{}, fmt.Errorf(
				"%w: %q is missing top-level openapi/swagger field",
				ErrInvalidOpenAPIDocument,
				candidatePath,
			)
		}

		if _, exists := rootSet[candidatePath]; exists {
			continue
		}
		rootSet[candidatePath] = struct{}{}
		roots = append(roots, candidatePath)
		rootDocuments[candidatePath] = content
	}

	if len(roots) == 0 {
		return ResolutionResult{
			OpenAPIChanged: hasDeletedCandidate,
			CandidateFiles: []string{},
			Documents:      map[string][]byte{},
		}, nil
	}

	documents := make(map[string][]byte, len(rootDocuments))
	for filePath, content := range rootDocuments {
		documents[filePath] = content
	}
	visitState := make(map[string]visitState, len(roots))

	for _, rootPath := range roots {
		if err := r.resolveRecursive(ctx, client, projectID, toSHA, rootPath, documents, visitState, nil); err != nil {
			return ResolutionResult{}, err
		}
	}

	return ResolutionResult{
		OpenAPIChanged: true,
		CandidateFiles: roots,
		Documents:      documents,
	}, nil
}

type visitState int

const (
	visitStateNone visitState = iota
	visitStateVisiting
	visitStateDone
)

func (r *Resolver) resolveRecursive(
	ctx context.Context,
	client GitLabClient,
	projectID int64,
	sha string,
	filePath string,
	documents map[string][]byte,
	states map[string]visitState,
	stack []string,
) error {
	switch states[filePath] {
	case visitStateDone:
		return nil
	case visitStateVisiting:
		cycle := appendCyclePath(stack, filePath)
		return fmt.Errorf("%w: %s", ErrReferenceCycle, strings.Join(cycle, " -> "))
	}

	if _, exists := documents[filePath]; !exists {
		if len(documents) >= r.maxFetches {
			return fmt.Errorf(
				"%w: max_fetches=%d reached while loading %q",
				ErrFetchLimitExceeded,
				r.maxFetches,
				filePath,
			)
		}
		content, err := client.GetFileContent(ctx, projectID, filePath, sha)
		if err != nil {
			return fmt.Errorf("fetch referenced file %q: %w", filePath, err)
		}
		documents[filePath] = content
	}

	states[filePath] = visitStateVisiting
	stack = append(stack, filePath)

	parsed, err := parseDocument(documents[filePath])
	if err != nil {
		return fmt.Errorf("%w: parse %q: %v", ErrInvalidOpenAPIDocument, filePath, err)
	}

	targets, err := collectLocalRefTargets(parsed, filePath)
	if err != nil {
		return err
	}
	for _, target := range targets {
		if err := r.resolveRecursive(ctx, client, projectID, sha, target, documents, states, stack); err != nil {
			return err
		}
	}

	states[filePath] = visitStateDone
	return nil
}

func detectCandidatePath(changedPath gitlab.ChangedPath) string {
	pathCandidate := strings.TrimSpace(changedPath.NewPath)
	if changedPath.DeletedFile {
		pathCandidate = strings.TrimSpace(changedPath.OldPath)
	}
	if pathCandidate == "" {
		pathCandidate = strings.TrimSpace(changedPath.OldPath)
	}
	return normalizeRepoPath(pathCandidate)
}

func normalizeRepoPath(raw string) string {
	trimmed := strings.TrimSpace(strings.TrimPrefix(raw, "/"))
	if trimmed == "" {
		return ""
	}

	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return ""
	}
	return cleaned
}

func (r *Resolver) matchesIncludeGlob(filePath string) (bool, error) {
	for _, glob := range r.includeGlobs {
		matches, err := doublestar.PathMatch(glob, filePath)
		if err != nil {
			return false, err
		}
		if matches {
			return true, nil
		}
	}
	return false, nil
}

func parseDocument(content []byte) (any, error) {
	var value any
	if err := yaml.Unmarshal(content, &value); err != nil {
		return nil, err
	}
	return normalizeYAMLValue(value), nil
}

func normalizeYAMLValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		normalized := make(map[string]any, len(typed))
		for key, nested := range typed {
			normalized[key] = normalizeYAMLValue(nested)
		}
		return normalized
	case map[any]any:
		normalized := make(map[string]any, len(typed))
		for rawKey, nested := range typed {
			key := strings.TrimSpace(fmt.Sprint(rawKey))
			normalized[key] = normalizeYAMLValue(nested)
		}
		return normalized
	case []any:
		normalized := make([]any, len(typed))
		for i := range typed {
			normalized[i] = normalizeYAMLValue(typed[i])
		}
		return normalized
	default:
		return value
	}
}

func hasTopLevelOpenAPIOrSwagger(document any) bool {
	root, ok := document.(map[string]any)
	if !ok {
		return false
	}
	if _, exists := root["openapi"]; exists {
		return true
	}
	if _, exists := root["swagger"]; exists {
		return true
	}
	return false
}

func collectLocalRefTargets(document any, sourcePath string) ([]string, error) {
	rawRefs := make([]string, 0)
	collectRefs(document, &rawRefs)

	seen := make(map[string]struct{}, len(rawRefs))
	targets := make([]string, 0, len(rawRefs))

	for _, rawRef := range rawRefs {
		target, err := resolveLocalRefTarget(sourcePath, rawRef)
		if err != nil {
			return nil, err
		}
		if target == "" {
			continue
		}
		if _, exists := seen[target]; exists {
			continue
		}
		seen[target] = struct{}{}
		targets = append(targets, target)
	}
	return targets, nil
}

func collectRefs(document any, refs *[]string) {
	switch typed := document.(type) {
	case map[string]any:
		for key, nested := range typed {
			if key == "$ref" {
				if ref, ok := nested.(string); ok {
					*refs = append(*refs, ref)
				}
				continue
			}
			collectRefs(nested, refs)
		}
	case []any:
		for _, nested := range typed {
			collectRefs(nested, refs)
		}
	}
}

func resolveLocalRefTarget(sourcePath string, rawRef string) (string, error) {
	ref := strings.TrimSpace(rawRef)
	if ref == "" {
		return "", nil
	}

	parsed, err := url.Parse(ref)
	if err != nil {
		return "", fmt.Errorf("%w: %q in %q is not a valid URI: %v", ErrInvalidReference, rawRef, sourcePath, err)
	}
	if parsed.Scheme != "" || parsed.Host != "" {
		return "", fmt.Errorf(
			"%w: external reference %q in %q is not supported",
			ErrInvalidReference,
			rawRef,
			sourcePath,
		)
	}
	if parsed.Path == "" {
		return "", nil
	}

	referencePath := parsed.Path
	if strings.HasPrefix(referencePath, "/") {
		referencePath = strings.TrimPrefix(path.Clean(referencePath), "/")
	} else {
		referencePath = path.Clean(path.Join(path.Dir(sourcePath), referencePath))
	}

	if referencePath == "." || referencePath == "" {
		return "", nil
	}
	if referencePath == ".." || strings.HasPrefix(referencePath, "../") {
		return "", fmt.Errorf(
			"%w: reference %q in %q escapes repository root",
			ErrInvalidReference,
			rawRef,
			sourcePath,
		)
	}
	return referencePath, nil
}

func appendCyclePath(stack []string, node string) []string {
	index := -1
	for i := range stack {
		if stack[i] == node {
			index = i
			break
		}
	}
	if index < 0 {
		cycle := append([]string{}, stack...)
		cycle = append(cycle, node)
		return cycle
	}
	cycle := append([]string{}, stack[index:]...)
	cycle = append(cycle, node)
	return cycle
}
