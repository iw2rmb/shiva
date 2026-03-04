package openapi

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/iw2rmb/shiva/internal/gitlab"
	"gopkg.in/yaml.v3"
)

const DefaultMaxFetches = 128
const DefaultBootstrapFetchConcurrency = 8
const DefaultBootstrapSniffBytes = 4096

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

type GitLabBootstrapClient interface {
	GitLabClient
	ListRepositoryTree(
		ctx context.Context,
		projectID int64,
		sha string,
		path string,
		recursive bool,
	) ([]gitlab.TreeEntry, error)
}

type ResolverConfig struct {
	IncludeGlobs              []string
	MaxFetches                int
	BootstrapFetchConcurrency int
	BootstrapSniffBytes       int
}

type Resolver struct {
	includeGlobs              []string
	maxFetches                int
	bootstrapFetchConcurrency int
	bootstrapSniffBytes       int
}

type ResolutionResult struct {
	OpenAPIChanged bool
	CandidateFiles []string
	Documents      map[string][]byte
}

type RootResolution struct {
	RootPath        string            `json:"root_path"`
	Documents       map[string][]byte `json:"documents"`
	DependencyFiles []string          `json:"dependency_files"`
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

	bootstrapFetchConcurrency := cfg.BootstrapFetchConcurrency
	if bootstrapFetchConcurrency < 0 {
		return nil, errors.New("openapi bootstrap fetch concurrency must be at least 1")
	}
	if bootstrapFetchConcurrency == 0 {
		bootstrapFetchConcurrency = DefaultBootstrapFetchConcurrency
	}

	bootstrapSniffBytes := cfg.BootstrapSniffBytes
	if bootstrapSniffBytes < 0 {
		return nil, errors.New("openapi bootstrap sniff bytes must be at least 1")
	}
	if bootstrapSniffBytes == 0 {
		bootstrapSniffBytes = DefaultBootstrapSniffBytes
	}

	return &Resolver{
		includeGlobs:              normalizedGlobs,
		maxFetches:                maxFetches,
		bootstrapFetchConcurrency: bootstrapFetchConcurrency,
		bootstrapSniffBytes:       bootstrapSniffBytes,
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

		if _, err := isOpenAPIRootDocument(content, candidatePath, true); err != nil {
			return ResolutionResult{}, err
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

	documents, err := r.resolveRootSet(ctx, client, projectID, toSHA, roots, rootDocuments)
	if err != nil {
		return ResolutionResult{}, err
	}

	return ResolutionResult{
		OpenAPIChanged: true,
		CandidateFiles: roots,
		Documents:      documents,
	}, nil
}

func (r *Resolver) ResolveRootOpenAPIAtSHA(
	ctx context.Context,
	client GitLabClient,
	projectID int64,
	sha string,
	rootPath string,
) (RootResolution, error) {
	normalizedSHA, err := validateResolveAtSHAInputs(client, projectID, sha)
	if err != nil {
		return RootResolution{}, err
	}

	normalizedRootPath := normalizeRepoPath(rootPath)
	if normalizedRootPath == "" {
		return RootResolution{}, errors.New("root path must not be empty")
	}

	rootDocument, err := client.GetFileContent(ctx, projectID, normalizedRootPath, normalizedSHA)
	if err != nil {
		return RootResolution{}, fmt.Errorf("fetch root %q: %w", normalizedRootPath, err)
	}

	if _, err := isOpenAPIRootDocument(rootDocument, normalizedRootPath, true); err != nil {
		return RootResolution{}, err
	}

	documents, err := r.resolveRootSet(
		ctx,
		client,
		projectID,
		normalizedSHA,
		[]string{normalizedRootPath},
		map[string][]byte{normalizedRootPath: rootDocument},
	)
	if err != nil {
		return RootResolution{}, err
	}

	return RootResolution{
		RootPath:        normalizedRootPath,
		Documents:       documents,
		DependencyFiles: listDependencyFiles(normalizedRootPath, documents),
	}, nil
}

func (r *Resolver) ResolveDiscoveredRootsAtPaths(
	ctx context.Context,
	client GitLabClient,
	projectID int64,
	sha string,
	paths []string,
) ([]RootResolution, error) {
	normalizedSHA, err := validateResolveAtSHAInputs(client, projectID, sha)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return []RootResolution{}, nil
	}

	fileIgnores, err := LoadShivaIgnoreAtSHA(ctx, client, projectID, normalizedSHA)
	if err != nil {
		return nil, err
	}
	effectiveIgnores := ComposeIgnoreGlobs(fileIgnores)

	candidatePaths := make([]string, 0, len(paths))
	candidateSet := make(map[string]struct{}, len(paths))

	for _, filePath := range paths {
		candidatePath := normalizeRepoPath(filePath)
		if candidatePath == "" {
			continue
		}

		ignored, err := ShouldIgnorePath(candidatePath, effectiveIgnores)
		if err != nil {
			return nil, fmt.Errorf("evaluate ignore rules for %q: %w", candidatePath, err)
		}
		if ignored {
			continue
		}
		if !hasOpenAPIExtension(candidatePath) {
			continue
		}
		if _, exists := candidateSet[candidatePath]; exists {
			continue
		}

		candidateSet[candidatePath] = struct{}{}
		candidatePaths = append(candidatePaths, candidatePath)
	}

	rootPaths, rootDocuments, err := r.resolveBootstrapRootCandidates(
		ctx,
		client,
		projectID,
		normalizedSHA,
		candidatePaths,
	)
	if err != nil {
		return nil, err
	}

	return r.resolveRootResolutions(ctx, client, projectID, normalizedSHA, rootPaths, rootDocuments)
}

func (r *Resolver) ResolveRepositoryOpenAPIAtSHA(
	ctx context.Context,
	client GitLabBootstrapClient,
	projectID int64,
	sha string,
) ([]RootResolution, error) {
	normalizedSHA, err := validateResolveAtSHAInputs(client, projectID, sha)
	if err != nil {
		return nil, err
	}

	treeEntries, err := client.ListRepositoryTree(ctx, projectID, normalizedSHA, "", true)
	if err != nil {
		return nil, fmt.Errorf("list repository tree: %w", err)
	}

	fileIgnores, err := LoadShivaIgnoreAtSHA(ctx, client, projectID, normalizedSHA)
	if err != nil {
		return nil, err
	}
	effectiveIgnores := ComposeIgnoreGlobs(fileIgnores)

	candidatePaths := make([]string, 0, len(treeEntries))
	candidateSet := make(map[string]struct{}, len(treeEntries))

	for _, entry := range treeEntries {
		candidatePath := normalizeRepoPath(entry.Path)
		if candidatePath == "" {
			continue
		}

		ignored, err := ShouldIgnorePath(candidatePath, effectiveIgnores)
		if err != nil {
			return nil, fmt.Errorf("evaluate ignore rules for %q: %w", candidatePath, err)
		}
		if ignored {
			continue
		}
		if !hasOpenAPIExtension(candidatePath) {
			continue
		}
		if _, exists := candidateSet[candidatePath]; exists {
			continue
		}
		candidateSet[candidatePath] = struct{}{}
		candidatePaths = append(candidatePaths, candidatePath)
	}

	rootPaths, rootDocuments, err := r.resolveBootstrapRootCandidates(
		ctx,
		client,
		projectID,
		normalizedSHA,
		candidatePaths,
	)
	if err != nil {
		return nil, err
	}

	return r.resolveRootResolutions(ctx, client, projectID, normalizedSHA, rootPaths, rootDocuments)
}

type bootstrapCandidateResult struct {
	path    string
	content []byte
	isRoot  bool
	err     error
}

func (r *Resolver) resolveBootstrapRootCandidates(
	ctx context.Context,
	client GitLabClient,
	projectID int64,
	sha string,
	candidatePaths []string,
) ([]string, map[string][]byte, error) {
	if len(candidatePaths) == 0 {
		return []string{}, map[string][]byte{}, nil
	}

	workerCount := r.bootstrapFetchConcurrency
	if workerCount > len(candidatePaths) {
		workerCount = len(candidatePaths)
	}

	jobs := make(chan string)
	results := make(chan bootstrapCandidateResult, len(candidatePaths))
	var workers sync.WaitGroup

	for i := 0; i < workerCount; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for candidatePath := range jobs {
				content, err := client.GetFileContent(ctx, projectID, candidatePath, sha)
				if err != nil {
					results <- bootstrapCandidateResult{
						path: candidatePath,
						err:  fmt.Errorf("fetch candidate %q: %w", candidatePath, err),
					}
					continue
				}
				if !sniffTopLevelOpenAPIOrSwagger(content, candidatePath, r.bootstrapSniffBytes) {
					results <- bootstrapCandidateResult{path: candidatePath}
					continue
				}

				isRoot, err := isOpenAPIRootDocument(content, candidatePath, false)
				if err != nil {
					results <- bootstrapCandidateResult{path: candidatePath, err: err}
					continue
				}
				if !isRoot {
					results <- bootstrapCandidateResult{path: candidatePath}
					continue
				}

				results <- bootstrapCandidateResult{
					path:    candidatePath,
					content: content,
					isRoot:  true,
				}
			}
		}()
	}

	for _, candidatePath := range candidatePaths {
		select {
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			close(results)
			return nil, nil, ctx.Err()
		case jobs <- candidatePath:
		}
	}
	close(jobs)
	workers.Wait()
	close(results)

	rootPaths := make([]string, 0, len(candidatePaths))
	rootDocuments := make(map[string][]byte, len(candidatePaths))
	failures := make([]bootstrapCandidateResult, 0)

	for result := range results {
		if result.err != nil {
			failures = append(failures, result)
			continue
		}
		if !result.isRoot {
			continue
		}
		rootPaths = append(rootPaths, result.path)
		rootDocuments[result.path] = result.content
	}

	if len(failures) > 0 {
		sort.Slice(failures, func(i, j int) bool {
			return failures[i].path < failures[j].path
		})
		return nil, nil, failures[0].err
	}

	sort.Strings(rootPaths)
	return rootPaths, rootDocuments, nil
}

func (r *Resolver) resolveRootResolutions(
	ctx context.Context,
	client GitLabClient,
	projectID int64,
	sha string,
	rootPaths []string,
	rootDocuments map[string][]byte,
) ([]RootResolution, error) {
	results := make([]RootResolution, 0, len(rootPaths))
	for _, rootPath := range rootPaths {
		documents, err := r.resolveRootSet(
			ctx,
			client,
			projectID,
			sha,
			[]string{rootPath},
			map[string][]byte{rootPath: rootDocuments[rootPath]},
		)
		if err != nil {
			return nil, err
		}

		results = append(results, RootResolution{
			RootPath:        rootPath,
			Documents:       documents,
			DependencyFiles: listDependencyFiles(rootPath, documents),
		})
	}
	return results, nil
}

type visitState int

const (
	visitStateNone visitState = iota
	visitStateVisiting
	visitStateDone
)

func (r *Resolver) resolveRootSet(
	ctx context.Context,
	client GitLabClient,
	projectID int64,
	sha string,
	roots []string,
	rootDocuments map[string][]byte,
) (map[string][]byte, error) {
	documents := make(map[string][]byte, len(rootDocuments))
	for filePath, content := range rootDocuments {
		documents[filePath] = content
	}
	visitState := make(map[string]visitState, len(roots))

	for _, rootPath := range roots {
		if err := r.resolveRecursive(ctx, client, projectID, sha, rootPath, documents, visitState, nil); err != nil {
			return nil, err
		}
	}

	return documents, nil
}

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

func validateResolveAtSHAInputs(client GitLabClient, projectID int64, sha string) (string, error) {
	if client == nil {
		return "", errors.New("gitlab client is required")
	}
	if projectID < 1 {
		return "", errors.New("project id must be positive")
	}

	normalizedSHA := strings.TrimSpace(sha)
	if normalizedSHA == "" {
		return "", errors.New("sha must not be empty")
	}

	return normalizedSHA, nil
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

func isOpenAPIRootDocument(content []byte, filePath string, strict bool) (bool, error) {
	parsed, err := parseDocument(content)
	if err != nil {
		if !strict {
			return false, nil
		}
		return false, fmt.Errorf("%w: parse %q: %v", ErrInvalidOpenAPIDocument, filePath, err)
	}

	if hasTopLevelOpenAPIOrSwagger(parsed) {
		return true, nil
	}

	if !strict {
		return false, nil
	}
	return false, fmt.Errorf(
		"%w: %q is missing top-level openapi/swagger field",
		ErrInvalidOpenAPIDocument,
		filePath,
	)
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

func hasOpenAPIExtension(filePath string) bool {
	switch strings.ToLower(path.Ext(filePath)) {
	case ".yaml", ".yml", ".json":
		return true
	default:
		return false
	}
}

func sniffTopLevelOpenAPIOrSwagger(content []byte, filePath string, maxSniffBytes int) bool {
	if len(content) == 0 {
		return false
	}

	prefix := content
	if len(prefix) > maxSniffBytes {
		prefix = prefix[:maxSniffBytes]
	}
	snippet := string(prefix)

	switch strings.ToLower(path.Ext(filePath)) {
	case ".json":
		return strings.Contains(snippet, `"openapi"`) || strings.Contains(snippet, `"swagger"`)
	case ".yaml", ".yml":
		lines := strings.Split(snippet, "\n")
		for _, rawLine := range lines {
			trimmed := strings.TrimSpace(rawLine)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			if rawLine != strings.TrimLeft(rawLine, " \t") {
				continue
			}
			if strings.HasPrefix(trimmed, "openapi:") || strings.HasPrefix(trimmed, "swagger:") {
				return true
			}
			if strings.HasPrefix(trimmed, "\"openapi\":") || strings.HasPrefix(trimmed, "\"swagger\":") {
				return true
			}
			if strings.HasPrefix(trimmed, "'openapi':") || strings.HasPrefix(trimmed, "'swagger':") {
				return true
			}
		}
	}

	return false
}

func listDependencyFiles(rootPath string, documents map[string][]byte) []string {
	dependencies := make([]string, 0, len(documents))
	for filePath := range documents {
		if filePath == rootPath {
			continue
		}
		dependencies = append(dependencies, filePath)
	}
	sort.Strings(dependencies)
	return dependencies
}
