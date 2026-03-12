package httpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/iw2rmb/shiva/internal/cli/request"
	"github.com/iw2rmb/shiva/internal/store"
)

type runtimeRepoLookup interface {
	GetRepoByNamespaceAndRepo(ctx context.Context, namespace string, repo string) (store.Repo, error)
}

type runtimeRoute struct {
	Repo             store.Repo
	OpenAPIPath      string
	SnapshotInput    store.ResolveReadSnapshotInput
	ExplicitSelector string
}

func (s *Server) handleRuntimeRoute(c *fiber.Ctx) error {
	route, err := parseRuntimeRoute(c.Context(), c.Path(), s.readStore)
	if err != nil {
		return s.writeQueryError(c, err)
	}

	if _, err := s.readStore.ResolveReadSnapshot(c.Context(), route.SnapshotInput); err != nil {
		return s.writeQueryError(c, err)
	}

	return c.Status(fiber.StatusNotImplemented).JSON(fiber.Map{
		"error": "runtime operation resolution is not implemented",
	})
}

func parseRuntimeRoute(ctx context.Context, path string, lookup runtimeRepoLookup) (runtimeRoute, error) {
	segments, err := splitRuntimeRoutePath(path)
	if err != nil {
		return runtimeRoute{}, err
	}

	for candidateLength := len(segments) - 1; candidateLength >= 2; candidateLength-- {
		namespace := strings.Join(segments[:candidateLength-1], "/")
		repo := segments[candidateLength-1]

		resolvedRepo, err := lookup.GetRepoByNamespaceAndRepo(ctx, namespace, repo)
		if err != nil {
			if errors.Is(err, store.ErrRepoNotFound) {
				continue
			}
			return runtimeRoute{}, err
		}

		return buildRuntimeRoute(resolvedRepo, segments[candidateLength:])
	}

	return runtimeRoute{}, fmt.Errorf("%w: runtime route path=%q", store.ErrRepoNotFound, path)
}

func buildRuntimeRoute(repo store.Repo, suffix []string) (runtimeRoute, error) {
	if len(suffix) == 0 {
		return runtimeRoute{}, invalidQuery("runtime route must include an OpenAPI path")
	}

	selector := ""
	if strings.HasPrefix(suffix[0], "@") {
		selector = strings.TrimPrefix(suffix[0], "@")
		if err := validateRuntimeSelector(selector); err != nil {
			return runtimeRoute{}, err
		}
		suffix = suffix[1:]
	}
	if len(suffix) == 0 {
		return runtimeRoute{}, invalidQuery("runtime route must include an OpenAPI path")
	}

	openAPIPath := "/" + strings.Join(suffix, "/")
	snapshotInput := store.ResolveReadSnapshotInput{
		Namespace: repo.Namespace,
		Repo:      repo.Repo,
	}
	if selector != "" && selector != runtimeSelectorLatest {
		snapshotInput.SHA = selector
	}

	return runtimeRoute{
		Repo:             repo,
		OpenAPIPath:      openAPIPath,
		SnapshotInput:    snapshotInput,
		ExplicitSelector: selector,
	}, nil
}

func splitRuntimeRoutePath(path string) ([]string, error) {
	path = strings.TrimSpace(path)
	if !strings.HasPrefix(path, runtimeRoutePrefix+"/") {
		return nil, invalidQuery("runtime route must start with /gl/")
	}

	rawSegments := strings.Split(strings.TrimPrefix(path, runtimeRoutePrefix+"/"), "/")
	if len(rawSegments) < 3 {
		return nil, invalidQuery("runtime route must include repo path and OpenAPI path")
	}

	segments := make([]string, 0, len(rawSegments))
	for _, segment := range rawSegments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			return nil, invalidQuery("runtime route segments must not be empty")
		}
		segments = append(segments, segment)
	}
	return segments, nil
}

func validateRuntimeSelector(selector string) error {
	switch {
	case selector == "":
		return invalidQuery("runtime selector must not be empty")
	case selector == runtimeSelectorLatest:
		return nil
	case request.IsShortSHA(selector):
		return nil
	default:
		return invalidQuery("runtime selector must be latest or exactly 8 lowercase hex characters")
	}
}
