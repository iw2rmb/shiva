package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v2"
	"gopkg.in/yaml.v3"

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

type contentFormat string

const (
	formatJSON contentFormat = "json"
	formatYAML contentFormat = "yaml"
)

var supportedHTTPMethods = map[string]struct{}{
	"get":     {},
	"post":    {},
	"put":     {},
	"patch":   {},
	"delete":  {},
	"head":    {},
	"options": {},
	"trace":   {},
}

func (s *Server) handleGetSpecNoSelector(c *fiber.Ctx) error {
	format, ok := parseContentFormat(c.Params("format"))
	if !ok {
		return c.SendStatus(fiber.StatusNotFound)
	}
	return s.handleGetSpec(c, true, "", format)
}

func (s *Server) handleGetSpecOrMethodSliceNoSelector(c *fiber.Ctx) error {
	format, ok := parseContentFormat(c.Params("format"))
	if !ok {
		return c.SendStatus(fiber.StatusNotFound)
	}

	selector, err := decodePathParam(c.Params("selector"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}
	method := normalizeHTTPMethod(selector)
	if method != "" {
		return s.handleGetMethodSlice(c, true, "", method, format)
	}

	return s.handleGetSpec(c, false, selector, format)
}

func (s *Server) handleGetMethodSliceBySelector(c *fiber.Ctx) error {
	format, ok := parseContentFormat(c.Params("format"))
	if !ok {
		return c.SendStatus(fiber.StatusNotFound)
	}

	selector, err := decodePathParam(c.Params("selector"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	method, isMethod, err := decodeHTTPMethodParam(c.Params("method"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}
	if !isMethod {
		return c.SendStatus(fiber.StatusNotFound)
	}

	return s.handleGetMethodSlice(c, false, selector, method, format)
}

func (s *Server) handleGetOperationNoSelector(c *fiber.Ctx) error {
	method, isMethod, err := decodeHTTPMethodParam(c.Params("method"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}
	if !isMethod {
		return c.Next()
	}

	return s.handleGetOperationSlice(c, true, "", method, c.Params("*"))
}

func (s *Server) handleGetOperationBySelector(c *fiber.Ctx) error {
	selector, err := decodePathParam(c.Params("selector"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	method, isMethod, err := decodeHTTPMethodParam(c.Params("method"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}
	if !isMethod {
		return c.Next()
	}

	return s.handleGetOperationSlice(c, false, selector, method, c.Params("*"))
}

func (s *Server) handleGetSpec(c *fiber.Ctx, noSelector bool, selector string, format contentFormat) error {
	resolved, err := s.resolveReadSelector(c, noSelector, selector)
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

	switch format {
	case formatJSON:
		c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSONCharsetUTF8)
		return c.Status(fiber.StatusOK).Send(artifact.SpecJSON)
	case formatYAML:
		c.Set(fiber.HeaderContentType, "application/yaml; charset=utf-8")
		return c.Status(fiber.StatusOK).SendString(artifact.SpecYAML)
	default:
		return c.SendStatus(fiber.StatusNotFound)
	}
}

func (s *Server) handleGetMethodSlice(
	c *fiber.Ctx,
	noSelector bool,
	selector string,
	method string,
	format contentFormat,
) error {
	resolved, err := s.resolveReadSelector(c, noSelector, selector)
	if err != nil {
		return s.writeReadRouteError(c, err)
	}

	endpoints, err := s.readStore.ListEndpointIndexByRevision(c.Context(), resolved.Revision.ID)
	if err != nil {
		return s.writeReadRouteError(c, err)
	}

	payload, found, err := buildMethodSlicePayload(endpoints, method)
	if err != nil {
		return s.writeReadRouteError(c, err)
	}
	if !found {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "method slice not found",
		})
	}

	return writeSliceResponse(c, format, payload)
}

func (s *Server) handleGetOperationSlice(
	c *fiber.Ctx,
	noSelector bool,
	selector string,
	method string,
	rawPath string,
) error {
	resolved, err := s.resolveReadSelector(c, noSelector, selector)
	if err != nil {
		return s.writeReadRouteError(c, err)
	}

	endpointPath, format, err := decodeEndpointPathAndFormat(rawPath)
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

	payload, err := buildOperationSlicePayload(endpoint)
	if err != nil {
		return s.writeReadRouteError(c, err)
	}

	return writeSliceResponse(c, format, payload)
}

func (s *Server) resolveReadSelector(
	c *fiber.Ctx,
	noSelector bool,
	selector string,
) (store.ResolvedReadSelector, error) {
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
		input.Selector = selector
	}

	return s.readStore.ResolveReadSelector(c.Context(), input)
}

func buildMethodSlicePayload(
	endpoints []store.EndpointIndexRecord,
	method string,
) (map[string]any, bool, error) {
	paths := make(map[string]map[string]any)
	for _, endpoint := range endpoints {
		if endpoint.Method != method {
			continue
		}

		operation, err := decodeOperationRawJSON(endpoint.RawJSON)
		if err != nil {
			return nil, false, err
		}

		if _, ok := paths[endpoint.Path]; !ok {
			paths[endpoint.Path] = make(map[string]any)
		}
		paths[endpoint.Path][method] = operation
	}

	if len(paths) == 0 {
		return nil, false, nil
	}

	return map[string]any{"paths": paths}, true, nil
}

func buildOperationSlicePayload(endpoint store.EndpointIndexRecord) (map[string]any, error) {
	operation, err := decodeOperationRawJSON(endpoint.RawJSON)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"paths": map[string]any{
			endpoint.Path: map[string]any{
				endpoint.Method: operation,
			},
		},
	}, nil
}

func decodeOperationRawJSON(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, errors.New("endpoint raw json must not be empty")
	}

	var operation any
	if err := json.Unmarshal(raw, &operation); err != nil {
		return nil, fmt.Errorf("unmarshal endpoint raw json: %w", err)
	}

	operationObject, ok := operation.(map[string]any)
	if !ok {
		return nil, errors.New("endpoint raw json must be an object")
	}

	return operationObject, nil
}

func writeSliceResponse(c *fiber.Ctx, format contentFormat, payload any) error {
	switch format {
	case formatJSON:
		return c.Status(fiber.StatusOK).JSON(payload)
	case formatYAML:
		body, err := yaml.Marshal(payload)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": fmt.Sprintf("marshal yaml response: %v", err),
			})
		}
		c.Set(fiber.HeaderContentType, "application/yaml; charset=utf-8")
		return c.Status(fiber.StatusOK).Send(body)
	default:
		return c.SendStatus(fiber.StatusNotFound)
	}
}

func decodeHTTPMethodParam(value string) (string, bool, error) {
	decoded, err := decodePathParam(value)
	if err != nil {
		return "", false, err
	}
	if decoded == "" {
		return "", false, nil
	}

	method := normalizeHTTPMethod(decoded)
	if method == "" {
		return strings.ToLower(strings.TrimSpace(decoded)), false, nil
	}
	return method, true, nil
}

func normalizeHTTPMethod(value string) string {
	method := strings.ToLower(strings.TrimSpace(value))
	if _, ok := supportedHTTPMethods[method]; !ok {
		return ""
	}
	return method
}

func decodeEndpointPathAndFormat(value string) (string, contentFormat, error) {
	decoded, err := decodePathParam(value)
	if err != nil {
		return "", "", err
	}
	if decoded == "" {
		return "", "", errors.New("endpoint path must not be empty")
	}

	format := formatJSON
	lowerDecoded := strings.ToLower(decoded)
	switch {
	case strings.HasSuffix(lowerDecoded, ".json"):
		decoded = decoded[:len(decoded)-len(".json")]
		format = formatJSON
	case strings.HasSuffix(lowerDecoded, ".yaml"):
		decoded = decoded[:len(decoded)-len(".yaml")]
		format = formatYAML
	}

	decoded = strings.TrimSpace(decoded)
	if decoded == "" {
		return "", "", errors.New("endpoint path must not be empty")
	}
	if !strings.HasPrefix(decoded, "/") {
		decoded = "/" + decoded
	}
	return decoded, format, nil
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

func parseContentFormat(value string) (contentFormat, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "json":
		return formatJSON, true
	case "yaml", "yml":
		return formatYAML, true
	default:
		return "", false
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
	case errors.Is(err, store.ErrSelectorNotFound),
		errors.Is(err, store.ErrSpecArtifactNotFound):
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
