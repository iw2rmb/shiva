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
	GetSpecArtifactByAPISpecRevisionID(ctx context.Context, apiSpecRevisionID int64) (store.SpecArtifact, error)
	ListAPISpecListingByRepo(ctx context.Context, repoID int64) ([]store.APISpecListing, error)
	ListAPISpecListingByRepoAtRevision(
		ctx context.Context,
		repoID int64,
		revisionID int64,
	) ([]store.APISpecListing, error)
	GetEndpointIndexByMethodPath(
		ctx context.Context,
		revisionID int64,
		method string,
		path string,
	) (store.EndpointIndexRecord, bool, error)
	GetEndpointIndexByMethodPathForAPISpecRevision(
		ctx context.Context,
		apiSpecRevisionID int64,
		method string,
		path string,
	) (store.EndpointIndexRecord, bool, error)
	GetAPISpecRevisionIDByRepoAndRootPath(ctx context.Context, repoID int64, apiRootPath string, revisionID int64) (int64, error)
}

type contentFormat string

const (
	formatJSON contentFormat = "json"
	formatYAML contentFormat = "yaml"
)

var readOperationHTTPMethods = []string{
	fiber.MethodGet,
	fiber.MethodPost,
	fiber.MethodPut,
	fiber.MethodPatch,
	fiber.MethodDelete,
	fiber.MethodHead,
	fiber.MethodOptions,
	fiber.MethodTrace,
}

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

func (s *Server) handleGetSpec(c *fiber.Ctx) error {
	parsed, err := parseReadRoutePath(c.Params("*"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	specTarget, format, ok := parseSpecTargetAndFormat(parsed.Target)
	if !ok {
		return c.SendStatus(fiber.StatusNotFound)
	}

	if specTarget == "" {
		return c.SendStatus(fiber.StatusNotFound)
	}

	selector, noSelector, err := parsedSelector(parsed)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	artifact, err := s.getSpecArtifact(c, selector, noSelector, parsed)
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

func (s *Server) handleListAPISpecsByRepo(c *fiber.Ctx) error {
	resolved, err := s.resolveReadSelector(c, true, "")
	if err != nil {
		return s.writeReadRouteError(c, err)
	}

	listing, err := s.readStore.ListAPISpecListingByRepo(c.Context(), resolved.RepoID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to load api listing",
		})
	}

	return c.Status(fiber.StatusOK).JSON(mapAPISpecListing(listing))
}

func (s *Server) handleListAPISpecsByRepoAtRevision(c *fiber.Ctx) error {
	resolved, err := s.resolveReadSelector(c, false, c.Params("selector"))
	if err != nil {
		return s.writeReadRouteError(c, err)
	}

	listing, err := s.readStore.ListAPISpecListingByRepoAtRevision(
		c.Context(),
		resolved.RepoID,
		resolved.Revision.ID,
	)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to load api listing",
		})
	}

	return c.Status(fiber.StatusOK).JSON(mapAPISpecListing(listing))
}

func (s *Server) handleOperationRoute(c *fiber.Ctx) error {
	parsed, err := parseReadRoutePath(c.Params("*"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	method, ok := operationMethodFromRequest(c)
	if !ok {
		return c.SendStatus(fiber.StatusMethodNotAllowed)
	}

	if parsed.HasSelector {
		return s.handleOperationRouteWithFallback(c, parsed, method)
	}

	return s.handleOperationRouteResolved(c, parsed, method, "", true)
}

func (s *Server) handleOperationRouteWithFallback(c *fiber.Ctx, parsed readRoutePath, method string) error {
	selector, err := decodePathParam(parsed.Selector)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	respErr := s.handleOperationRouteResolved(c, parsed, method, selector, false)
	if respErr == nil {
		return nil
	}

	var selectorErr *store.SelectorResolutionError
	if !errors.As(respErr, &selectorErr) || selectorErr.Code != store.SelectorResolutionNotFound {
		return s.writeReadRouteError(c, respErr)
	}

	return s.handleOperationRouteResolved(c, parsed, method, "", true)
}

func (s *Server) handleOperationRouteResolved(
	c *fiber.Ctx,
	parsed readRoutePath,
	method string,
	selector string,
	noSelector bool,
) error {
	endpointPath, format, err := decodeEndpointPathAndFormat(parsed.Target)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	resolvedRevisionID, endpointFinder, err := s.resolveReadRevisionID(c, parsed, selector, noSelector)
	if err != nil {
		if noSelector {
			return s.writeReadRouteError(c, err)
		}
		return err
	}

	var endpoint store.EndpointIndexRecord
	var found bool
	endpoint, found, err = endpointFinder(c.Context(), resolvedRevisionID, method, endpointPath)
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

func (s *Server) getSpecArtifact(c *fiber.Ctx, selector string, noSelector bool, parsed readRoutePath) (store.SpecArtifact, error) {
	resolvedRevisionID, _, err := s.resolveReadRevisionID(c, parsed, selector, noSelector)
	if err != nil {
		return store.SpecArtifact{}, err
	}

	if parsed.APIRoot == "" {
		return s.readStore.GetSpecArtifactByRevisionID(c.Context(), resolvedRevisionID)
	}

	return s.readStore.GetSpecArtifactByAPISpecRevisionID(c.Context(), resolvedRevisionID)
}

func (s *Server) resolveReadRevisionID(
	c *fiber.Ctx,
	parsed readRoutePath,
	selector string,
	noSelector bool,
) (int64, func(context.Context, int64, string, string) (store.EndpointIndexRecord, bool, error), error) {
	resolved, err := s.resolveReadSelector(c, noSelector, selector)
	if err != nil {
		return 0, nil, err
	}

	if parsed.APIRoot == "" {
		return resolved.Revision.ID, s.readStore.GetEndpointIndexByMethodPath, nil
	}

	apiSpecRevisionID, err := s.readStore.GetAPISpecRevisionIDByRepoAndRootPath(
		c.Context(),
		resolved.RepoID,
		parsed.APIRoot,
		resolved.Revision.ID,
	)
	if err != nil {
		return 0, nil, err
	}

	return apiSpecRevisionID, s.readStore.GetEndpointIndexByMethodPathForAPISpecRevision, nil
}

func parsedSelector(parsed readRoutePath) (selector string, noSelector bool, err error) {
	noSelector = !parsed.HasSelector
	if !parsed.HasSelector {
		return "", true, nil
	}

	selector, err = decodePathParam(parsed.Selector)
	if err != nil {
		return "", true, err
	}
	return selector, false, nil
}

func operationMethodFromRequest(c *fiber.Ctx) (string, bool) {
	method := normalizeHTTPMethod(c.Method())
	if method == "" {
		return "", false
	}
	return method, true
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

func parseSpecTargetAndFormat(value string) (string, contentFormat, bool) {
	decoded, err := decodePathParam(value)
	if err != nil || decoded == "" {
		return "", "", false
	}

	format := formatJSON
	lowerDecoded := strings.ToLower(decoded)
	switch {
	case strings.HasSuffix(lowerDecoded, ".json"):
		decoded = decoded[:len(decoded)-len(".json")]
	case strings.HasSuffix(lowerDecoded, ".yaml"):
		decoded = decoded[:len(decoded)-len(".yaml")]
		format = formatYAML
	default:
		return "", "", false
	}

	decoded = strings.TrimSpace(decoded)
	if decoded == "" {
		return "", "", false
	}

	switch decoded {
	case "openapi", "index":
		return decoded, format, true
	default:
		return "", "", false
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

func mapAPISpecListing(listings []store.APISpecListing) []apiSpecListingResponse {
	response := make([]apiSpecListingResponse, 0, len(listings))
	for _, listing := range listings {
		item := apiSpecListingResponse{
			API:    listing.API,
			Status: listing.Status,
		}
		if listing.LastProcessedRevision != nil {
			item.LastProcessedRevision = &apiSpecRevisionMetadataResponse{
				APISpecRevisionID: listing.LastProcessedRevision.APISpecRevisionID,
				IngestEventID:     listing.LastProcessedRevision.IngestEventID,
				IngestEventSHA:    listing.LastProcessedRevision.IngestEventSHA,
				IngestEventBranch: listing.LastProcessedRevision.IngestEventBranch,
			}
		}
		response = append(response, item)
	}

	return response
}

type apiSpecListingResponse struct {
	API                   string                           `json:"api"`
	Status                string                           `json:"status"`
	LastProcessedRevision *apiSpecRevisionMetadataResponse `json:"last_processed_revision"`
}

type apiSpecRevisionMetadataResponse struct {
	APISpecRevisionID int64  `json:"api_spec_revision_id"`
	IngestEventID     int64  `json:"ingest_event_id"`
	IngestEventSHA    string `json:"ingest_event_sha"`
	IngestEventBranch string `json:"ingest_event_branch"`
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
		errors.Is(err, store.ErrSpecArtifactNotFound),
		errors.Is(err, store.ErrAPISpecNotFound):
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
