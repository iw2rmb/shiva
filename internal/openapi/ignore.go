package openapi

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/iw2rmb/shiva/internal/gitlab"
)

var defaultIgnoreGlobs = []string{
	"**/test*/**",
	"**/__tests__/**",
	"**/node_modules/**",
	"**/vendor/**",
}

var (
	ErrMalformedShivaIgnoreLine       = errors.New("malformed .shivaignore line")
	ErrShivaIgnoreNegationUnsupported = errors.New("shivaignore negation is not supported")
)

func DefaultIgnoreGlobs() []string {
	globs := make([]string, len(defaultIgnoreGlobs))
	copy(globs, defaultIgnoreGlobs)
	return globs
}

func LoadShivaIgnoreAtSHA(
	ctx context.Context,
	client GitLabClient,
	projectID int64,
	sha string,
) ([]string, error) {
	if client == nil {
		return nil, errors.New("gitlab client is required")
	}
	if projectID < 1 {
		return nil, errors.New("project id must be positive")
	}
	normalizedSHA := strings.TrimSpace(sha)
	if normalizedSHA == "" {
		return nil, errors.New("sha must not be empty")
	}

	content, err := client.GetFileContent(ctx, projectID, "/.shivaignore", normalizedSHA)
	if err != nil {
		if errors.Is(err, gitlab.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("load .shivaignore: %w", err)
	}

	return ParseShivaIgnore(content)
}

func ComposeIgnoreGlobs(fileIgnores []string) []string {
	ignores := make([]string, 0, len(defaultIgnoreGlobs)+len(fileIgnores))
	ignores = append(ignores, defaultIgnoreGlobs...)
	ignores = append(ignores, fileIgnores...)
	return ignores
}

func ParseShivaIgnore(content []byte) ([]string, error) {
	if len(content) == 0 {
		return []string{}, nil
	}

	lines := strings.Split(string(content), "\n")
	patterns := make([]string, 0, len(lines))
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}

		pattern := strings.TrimLeft(trimmed, "/")
		if pattern == "" {
			return nil, fmt.Errorf("%w at line %d", ErrMalformedShivaIgnoreLine, i+1)
		}
		if strings.HasPrefix(pattern, "!") {
			return nil, fmt.Errorf("%w at line %d", ErrShivaIgnoreNegationUnsupported, i+1)
		}
		if !doublestar.ValidatePathPattern(pattern) {
			return nil, fmt.Errorf("%w at line %d: %q", ErrMalformedShivaIgnoreLine, i+1, trimmed)
		}

		patterns = append(patterns, pattern)
	}
	return patterns, nil
}

func ShouldIgnorePath(filePath string, ignoreGlobs []string) (bool, error) {
	normalizedPath := normalizeRepoPath(filePath)
	if normalizedPath == "" {
		return false, nil
	}

	for _, glob := range ignoreGlobs {
		matches, err := doublestar.PathMatch(glob, normalizedPath)
		if err != nil {
			return false, fmt.Errorf("invalid ignore glob %q: %w", glob, err)
		}
		if matches {
			return true, nil
		}
	}

	return false, nil
}
