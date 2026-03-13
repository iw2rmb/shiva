package lint

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	vacuumModel "github.com/daveshanley/vacuum/model"
	"github.com/daveshanley/vacuum/motor"
	"github.com/daveshanley/vacuum/rulesets"
)

type SourceIssue struct {
	RuleID   string
	Severity string
	Message  string
	JSONPath string
	FilePath string
	RangePos [4]int32
}

type SourceExecutionResult struct {
	Issues  []SourceIssue
	Failure *CanonicalFailure
}

type SourceRunner struct {
	ruleset           *rulesets.RuleSet
	logger            *slog.Logger
	timeout           time.Duration
	nodeLookupTimeout time.Duration
}

var defaultSourceRunner = NewSourceRunner()

func DefaultSourceRunner() *SourceRunner {
	return defaultSourceRunner
}

func NewSourceRunner() *SourceRunner {
	silentLogger := slog.New(slog.NewTextHandler(io.Discard, nil))

	return &SourceRunner{
		ruleset:           newDefaultOpenAPIRuleSet(silentLogger),
		logger:            silentLogger,
		timeout:           defaultRuleTimeout,
		nodeLookupTimeout: defaultNodeLookupTimeout,
	}
}

func (r *SourceRunner) RunSourceLayoutRoot(
	ctx context.Context,
	rootPath string,
	documents map[string][]byte,
) (SourceExecutionResult, error) {
	if err := ctx.Err(); err != nil {
		return SourceExecutionResult{}, err
	}

	normalizedRootPath, normalizedDocuments, err := normalizeSourceDocuments(rootPath, documents)
	if err != nil {
		return SourceExecutionResult{}, err
	}

	tempDir, err := os.MkdirTemp("", "shiva-vacuum-*")
	if err != nil {
		return SourceExecutionResult{}, fmt.Errorf("create source validation workspace: %w", err)
	}
	defer os.RemoveAll(tempDir)

	repoPathByAbsolutePath := make(map[string]string, len(normalizedDocuments))
	for repoPath, content := range normalizedDocuments {
		absolutePath := filepath.Join(tempDir, filepath.FromSlash(repoPath))
		if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
			return SourceExecutionResult{}, fmt.Errorf("create workspace directory for %q: %w", repoPath, err)
		}
		if err := os.WriteFile(absolutePath, content, 0o644); err != nil {
			return SourceExecutionResult{}, fmt.Errorf("write workspace file %q: %w", repoPath, err)
		}
		repoPathByAbsolutePath[filepath.Clean(absolutePath)] = repoPath
	}

	rootAbsolutePath, ok := repoPathByAbsolutePath[filepath.Clean(filepath.Join(tempDir, filepath.FromSlash(normalizedRootPath)))]
	if !ok {
		return SourceExecutionResult{}, fmt.Errorf("workspace root %q was not materialized", normalizedRootPath)
	}

	execution := &motor.RuleSetExecution{
		RuleSet:           r.ruleset,
		SpecFileName:      filepath.Join(tempDir, filepath.FromSlash(rootAbsolutePath)),
		Spec:              append([]byte(nil), normalizedDocuments[normalizedRootPath]...),
		Base:              filepath.Dir(filepath.Join(tempDir, filepath.FromSlash(rootAbsolutePath))),
		AllowLookup:       true,
		SilenceLogs:       true,
		Logger:            r.logger,
		Timeout:           r.timeout,
		NodeLookupTimeout: r.nodeLookupTimeout,
	}

	result := motor.ApplyRulesToRuleSet(execution)
	if err := ctx.Err(); err != nil {
		return SourceExecutionResult{}, err
	}

	if failure := normalizeCanonicalFailure(result.Errors); failure != nil {
		return SourceExecutionResult{Failure: failure}, nil
	}

	return SourceExecutionResult{
		Issues: normalizeSourceIssues(result.Results, normalizedRootPath, repoPathByAbsolutePath),
	}, nil
}

func normalizeSourceDocuments(
	rootPath string,
	documents map[string][]byte,
) (string, map[string][]byte, error) {
	normalizedRootPath := normalizeSourceRepoPath(rootPath)
	if normalizedRootPath == "" {
		return "", nil, errors.New("root path must not be empty")
	}
	if len(documents) == 0 {
		return "", nil, errors.New("source documents must not be empty")
	}

	normalizedDocuments := make(map[string][]byte, len(documents))
	for filePath, content := range documents {
		normalizedFilePath := normalizeSourceRepoPath(filePath)
		if normalizedFilePath == "" {
			return "", nil, fmt.Errorf("document path %q must be repo-relative", filePath)
		}
		if len(content) == 0 {
			return "", nil, fmt.Errorf("document %q must not be empty", normalizedFilePath)
		}
		normalizedDocuments[normalizedFilePath] = append([]byte(nil), content...)
	}

	if _, exists := normalizedDocuments[normalizedRootPath]; !exists {
		return "", nil, fmt.Errorf("root path %q is missing from source documents", normalizedRootPath)
	}

	return normalizedRootPath, normalizedDocuments, nil
}

func normalizeSourceIssues(
	results []vacuumModel.RuleFunctionResult,
	defaultFilePath string,
	repoPathByAbsolutePath map[string]string,
) []SourceIssue {
	issues := make([]SourceIssue, 0, len(results))
	for _, result := range results {
		severity := strings.TrimSpace(result.RuleSeverity)
		if severity == "" && result.Rule != nil {
			severity = strings.TrimSpace(result.Rule.Severity)
		}

		issues = append(issues, SourceIssue{
			RuleID:   strings.TrimSpace(result.RuleId),
			Severity: severity,
			Message:  strings.TrimSpace(result.Message),
			JSONPath: strings.TrimSpace(result.Path),
			FilePath: mapSourceIssueFilePath(result, defaultFilePath, repoPathByAbsolutePath),
			RangePos: sourceIssueRange(result),
		})
	}

	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].FilePath != issues[j].FilePath {
			return issues[i].FilePath < issues[j].FilePath
		}
		if issues[i].RangePos != issues[j].RangePos {
			for idx := range issues[i].RangePos {
				if issues[i].RangePos[idx] == issues[j].RangePos[idx] {
					continue
				}
				return issues[i].RangePos[idx] < issues[j].RangePos[idx]
			}
		}
		if issues[i].RuleID != issues[j].RuleID {
			return issues[i].RuleID < issues[j].RuleID
		}
		if issues[i].JSONPath != issues[j].JSONPath {
			return issues[i].JSONPath < issues[j].JSONPath
		}
		return issues[i].Message < issues[j].Message
	})

	return issues
}

func mapSourceIssueFilePath(
	result vacuumModel.RuleFunctionResult,
	defaultFilePath string,
	repoPathByAbsolutePath map[string]string,
) string {
	if result.Origin == nil {
		return defaultFilePath
	}

	for _, absolutePath := range []string{result.Origin.AbsoluteLocation, result.Origin.AbsoluteLocationValue} {
		absolutePath = strings.TrimSpace(absolutePath)
		if absolutePath == "" {
			continue
		}
		if repoPath, exists := repoPathByAbsolutePath[filepath.Clean(absolutePath)]; exists {
			return repoPath
		}
	}

	return defaultFilePath
}

func sourceIssueRange(result vacuumModel.RuleFunctionResult) [4]int32 {
	rangePos := [4]int32{
		int32(result.Range.Start.Line),
		int32(result.Range.Start.Char),
		int32(result.Range.End.Line),
		int32(result.Range.End.Char),
	}

	if result.Origin == nil {
		return normalizeSourceRange(rangePos)
	}

	if result.Origin.Line > 0 {
		rangePos[0] = int32(result.Origin.Line)
		rangePos[2] = int32(result.Origin.Line)
	}
	if result.Origin.Column > 0 {
		rangePos[1] = int32(result.Origin.Column)
		rangePos[3] = int32(result.Origin.Column)
	}
	if result.Origin.ValueNode != nil {
		if result.Origin.ValueNode.Line > 0 {
			rangePos[2] = int32(result.Origin.ValueNode.Line)
		}
		if result.Origin.ValueNode.Column > 0 {
			rangePos[3] = int32(result.Origin.ValueNode.Column)
		}
	} else if result.Origin.Node != nil {
		if result.Origin.Node.Line > 0 {
			rangePos[2] = int32(result.Origin.Node.Line)
		}
		if result.Origin.Node.Column > 0 {
			rangePos[3] = int32(result.Origin.Node.Column + len(result.Origin.Node.Value))
		}
	}

	return normalizeSourceRange(rangePos)
}

func normalizeSourceRange(rangePos [4]int32) [4]int32 {
	if rangePos[0] < 1 {
		rangePos[0] = 1
	}
	if rangePos[1] < 1 {
		rangePos[1] = 1
	}
	if rangePos[2] < rangePos[0] {
		rangePos[2] = rangePos[0]
	}
	if rangePos[2] == rangePos[0] && rangePos[3] < rangePos[1] {
		rangePos[3] = rangePos[1]
	}
	if rangePos[3] < 1 {
		rangePos[3] = rangePos[1]
		if rangePos[3] < 1 {
			rangePos[3] = 1
		}
	}
	return rangePos
}

func normalizeSourceRepoPath(raw string) string {
	trimmed := strings.TrimPrefix(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return ""
	}

	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return ""
	}
	return cleaned
}

func newDefaultOpenAPIRuleSet(logger *slog.Logger) *rulesets.RuleSet {
	return rulesets.BuildDefaultRuleSetsWithLogger(logger).GenerateOpenAPIDefaultRuleSet()
}
