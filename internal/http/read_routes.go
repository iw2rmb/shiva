package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/iw2rmb/shiva/internal/store"
)

type readRouteStore interface {
	ResolveReadSelector(ctx context.Context, input store.ResolveReadSelectorInput) (store.ResolvedReadSelector, error)
	GetSpecArtifactByRevisionID(ctx context.Context, revisionID int64) (store.SpecArtifact, error)
	ListEndpointIndexByRevision(ctx context.Context, revisionID int64) ([]store.EndpointIndexRecord, error)
	GetEndpointIndexByMethodPath(
		ctx context.Context,
		revisionID int64,
		method string,
		path string,
	) (store.EndpointIndexRecord, bool, error)
}

type endpointResponse struct {
	Method      string          `json:"method"`
	Path        string          `json:"path"`
	OperationID string          `json:"operation_id,omitempty"`
	Summary     string          `json:"summary,omitempty"`
	Deprecated  bool            `json:"deprecated"`
	RawJSON     json.RawMessage `json:"raw_json"`
}

func (s *Server) handleGetSpecJSON(c *fiber.Ctx) error {
	resolved, err := s.resolveReadSelector(c, false)
	if err != nil {
		return s.writeReadRouteError(c, err)
	}

	artifact, err := s.readStore.GetSpecArtifactByRevisionID(c.Context(), resolved.Revision.ID)
	if err != nil {
		return s.writeReadRouteError(c, err)
	}

	c.Set(fiber.HeaderETag, artifact.ETag)
	if ifNoneMatchMatches(c.Get(fiber.HeaderIfNoneMatch), artifact.ETag) {
		return c.SendStatus(fiber.StatusNotModified)
	}

	c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSONCharsetUTF8)
	return c.Status(fiber.StatusOK).Send(artifact.SpecJSON)
}

func (s *Server) handleGetSpecYAML(c *fiber.Ctx) error {
	resolved, err := s.resolveReadSelector(c, false)
	if err != nil {
		return s.writeReadRouteError(c, err)
	}

	artifact, err := s.readStore.GetSpecArtifactByRevisionID(c.Context(), resolved.Revision.ID)
	if err != nil {
		return s.writeReadRouteError(c, err)
	}

	c.Set(fiber.HeaderETag, artifact.ETag)
	if ifNoneMatchMatches(c.Get(fiber.HeaderIfNoneMatch), artifact.ETag) {
		return c.SendStatus(fiber.StatusNotModified)
	}

	c.Set(fiber.HeaderContentType, "application/yaml; charset=utf-8")
	return c.Status(fiber.StatusOK).SendString(artifact.SpecYAML)
}

func (s *Server) handleListEndpointsBySelector(c *fiber.Ctx) error {
	resolved, err := s.resolveReadSelector(c, false)
	if err != nil {
		return s.writeReadRouteError(c, err)
	}

	endpoints, err := s.readStore.ListEndpointIndexByRevision(c.Context(), resolved.Revision.ID)
	if err != nil {
		return s.writeReadRouteError(c, err)
	}

	return c.Status(fiber.StatusOK).JSON(mapEndpoints(endpoints))
}

func (s *Server) handleGetEndpointBySelector(c *fiber.Ctx) error {
	resolved, err := s.resolveReadSelector(c, false)
	if err != nil {
		return s.writeReadRouteError(c, err)
	}

	method := strings.ToLower(strings.TrimSpace(c.Params("method")))
	if method == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "method must not be empty",
		})
	}

	endpointPath, err := decodeEndpointPathParam(c.Params("*"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	endpoint, found, err := s.readStore.GetEndpointIndexByMethodPath(
		c.Context(),
		resolved.Revision.ID,
		method,
		endpointPath,
	)
	if err != nil {
		return s.writeReadRouteError(c, err)
	}
	if !found {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "endpoint not found",
		})
	}

	return c.Status(fiber.StatusOK).JSON(mapEndpoint(endpoint))
}

func (s *Server) handleListEndpointsNoSelector(c *fiber.Ctx) error {
	resolved, err := s.resolveReadSelector(c, true)
	if err != nil {
		return s.writeReadRouteError(c, err)
	}

	endpoints, err := s.readStore.ListEndpointIndexByRevision(c.Context(), resolved.Revision.ID)
	if err != nil {
		return s.writeReadRouteError(c, err)
	}

	return c.Status(fiber.StatusOK).JSON(mapEndpoints(endpoints))
}

func (s *Server) resolveReadSelector(c *fiber.Ctx, noSelector bool) (store.ResolvedReadSelector, error) {
	if s.readStore == nil {
		return store.ResolvedReadSelector{}, errors.New("read store is not configured")
	}

	tenantKey, err := decodePathParam(c.Params("tenant"))
	if err != nil {
		return store.ResolvedReadSelector{}, &store.SelectorResolutionError{
			Code: store.SelectorResolutionInvalidInput,
			Err:  fmt.Errorf("decode tenant path parameter: %w", err),
		}
	}
	repoPath, err := decodePathParam(c.Params("repo"))
	if err != nil {
		return store.ResolvedReadSelector{}, &store.SelectorResolutionError{
			Code:      store.SelectorResolutionInvalidInput,
			TenantKey: tenantKey,
			Err:       fmt.Errorf("decode repo path parameter: %w", err),
		}
	}

	input := store.ResolveReadSelectorInput{
		TenantKey:  tenantKey,
		RepoPath:   repoPath,
		NoSelector: noSelector,
	}
	if !noSelector {
		selector, selectorErr := decodePathParam(c.Params("selector"))
		if selectorErr != nil {
			return store.ResolvedReadSelector{}, &store.SelectorResolutionError{
				Code:      store.SelectorResolutionInvalidInput,
				TenantKey: tenantKey,
				RepoPath:  repoPath,
				Err:       fmt.Errorf("decode selector path parameter: %w", selectorErr),
			}
		}
		input.Selector = selector
	}

	return s.readStore.ResolveReadSelector(c.Context(), input)
}

func decodeEndpointPathParam(value string) (string, error) {
	decoded, err := decodePathParam(value)
	if err != nil {
		return "", err
	}
	if decoded == "" {
		return "", errors.New("endpoint path must not be empty")
	}
	if !strings.HasPrefix(decoded, "/") {
		decoded = "/" + decoded
	}
	return decoded, nil
}

func decodePathParam(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}

	decoded, err := url.PathUnescape(value)
	if err != nil {
		return "", fmt.Errorf("unescape path parameter %q: %w", value, err)
	}
	return strings.TrimSpace(decoded), nil
}

func mapEndpoints(records []store.EndpointIndexRecord) []endpointResponse {
	mapped := make([]endpointResponse, 0, len(records))
	for _, record := range records {
		mapped = append(mapped, mapEndpoint(record))
	}
	return mapped
}

func mapEndpoint(record store.EndpointIndexRecord) endpointResponse {
	return endpointResponse{
		Method:      record.Method,
		Path:        record.Path,
		OperationID: record.OperationID,
		Summary:     record.Summary,
		Deprecated:  record.Deprecated,
		RawJSON:     json.RawMessage(record.RawJSON),
	}
}

func ifNoneMatchMatches(ifNoneMatch string, etag string) bool {
	ifNoneMatch = strings.TrimSpace(ifNoneMatch)
	etag = strings.TrimSpace(etag)
	if ifNoneMatch == "" || etag == "" {
		return false
	}

	for _, candidate := range strings.Split(ifNoneMatch, ",") {
		tag := strings.TrimSpace(candidate)
		if tag == "*" {
			return true
		}
		if normalizeETag(tag) == normalizeETag(etag) {
			return true
		}
	}
	return false
}

func normalizeETag(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "W/") {
		return strings.TrimSpace(strings.TrimPrefix(value, "W/"))
	}
	return value
}

func (s *Server) writeReadRouteError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, store.ErrStoreNotConfigured):
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "database is not configured",
		})
	case errors.Is(err, store.ErrSelectorInvalidInput):
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	case errors.Is(err, store.ErrSelectorUnprocessed):
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{
			"error": "selector points to unprocessed revision",
		})
	case errors.Is(err, store.ErrSelectorNotFound):
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "selector has no processed artifact",
		})
	case errors.Is(err, store.ErrSpecArtifactNotFound):
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "selector has no processed artifact",
		})
	default:
		if s.logger != nil {
			s.logger.Error("read route failed", "error", err)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "internal server error",
		})
	}
}
