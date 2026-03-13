package gitlab

import (
	"path"
	"sort"
	"strings"
)

type OpenAPIRootDependencySet struct {
	RootPath        string
	DependencyPaths []string
}

type ImpactedOpenAPIRoot struct {
	RootPath    string
	RootDeleted bool
}

func ImpactedOpenAPIRoots(
	roots []OpenAPIRootDependencySet,
	changedPaths []ChangedPath,
) []ImpactedOpenAPIRoot {
	changedPathSet := make(map[string]struct{}, len(changedPaths)*2)
	deletedRoots := make(map[string]struct{}, len(changedPaths))
	for _, changedPath := range changedPaths {
		for _, impactPath := range ChangedPathImpactPaths(changedPath) {
			changedPathSet[impactPath] = struct{}{}
		}
		deletedPath := DeletedChangedPath(changedPath)
		if deletedPath != "" {
			deletedRoots[deletedPath] = struct{}{}
		}
	}

	impacted := make([]ImpactedOpenAPIRoot, 0, len(roots))
	for _, root := range roots {
		rootPath := NormalizeRepoPath(root.RootPath)
		if rootPath == "" {
			continue
		}

		if !pathSetIntersects(changedPathSet, rootPath, root.DependencyPaths) {
			continue
		}

		_, rootDeleted := deletedRoots[rootPath]
		impacted = append(impacted, ImpactedOpenAPIRoot{
			RootPath:    rootPath,
			RootDeleted: rootDeleted,
		})
	}

	sort.SliceStable(impacted, func(i, j int) bool {
		if impacted[i].RootPath == impacted[j].RootPath {
			if impacted[i].RootDeleted == impacted[j].RootDeleted {
				return false
			}
			return !impacted[i].RootDeleted && impacted[j].RootDeleted
		}
		return impacted[i].RootPath < impacted[j].RootPath
	})

	return impacted
}

func ChangedPathImpactPaths(changedPath ChangedPath) []string {
	paths := make([]string, 0, 2)
	addPath := func(raw string) {
		normalized := NormalizeRepoPath(raw)
		if normalized == "" {
			return
		}
		for _, existing := range paths {
			if existing == normalized {
				return
			}
		}
		paths = append(paths, normalized)
	}

	addPath(changedPath.NewPath)
	addPath(changedPath.OldPath)
	return paths
}

func DeletedChangedPath(changedPath ChangedPath) string {
	if !changedPath.DeletedFile {
		return ""
	}

	pathCandidate := strings.TrimSpace(changedPath.OldPath)
	if pathCandidate == "" {
		pathCandidate = strings.TrimSpace(changedPath.NewPath)
	}
	return NormalizeRepoPath(pathCandidate)
}

func FallbackDiscoveryCandidatePaths(changedPaths []ChangedPath) []string {
	candidates := make([]string, 0, len(changedPaths))
	seen := make(map[string]struct{}, len(changedPaths))

	for _, changedPath := range changedPaths {
		if !changedPath.NewFile && !changedPath.RenamedFile {
			continue
		}

		pathCandidate := NormalizeRepoPath(changedPath.NewPath)
		if pathCandidate == "" {
			pathCandidate = NormalizeRepoPath(changedPath.OldPath)
		}
		if pathCandidate == "" {
			continue
		}

		if _, exists := seen[pathCandidate]; exists {
			continue
		}
		seen[pathCandidate] = struct{}{}
		candidates = append(candidates, pathCandidate)
	}

	sort.Strings(candidates)
	return candidates
}

func NormalizeRepoPath(raw string) string {
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

func pathSetIntersects(changedPathSet map[string]struct{}, rootPath string, dependencyPaths []string) bool {
	if _, exists := changedPathSet[rootPath]; exists {
		return true
	}

	for _, dependencyPath := range dependencyPaths {
		normalizedDependencyPath := NormalizeRepoPath(dependencyPath)
		if normalizedDependencyPath == "" {
			continue
		}
		if _, exists := changedPathSet[normalizedDependencyPath]; exists {
			return true
		}
	}

	return false
}
