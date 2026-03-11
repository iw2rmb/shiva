package httpserver

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/iw2rmb/shiva/internal/store"
)

type queryValidationError struct {
	message string
}

func (e *queryValidationError) Error() string {
	if e == nil {
		return "invalid query"
	}
	return e.message
}

type normalizedSpecQuery struct {
	Snapshot store.ResolveReadSnapshotInput
	Format   contentFormat
}

type normalizedOperationSelector struct {
	OperationID string
	Method      string
	Path        string
}

func parseSpecQuery(c *fiber.Ctx) (normalizedSpecQuery, error) {
	if err := rejectUnsupportedQueryParams(c, "operation_id", "method", "path"); err != nil {
		return normalizedSpecQuery{}, err
	}

	snapshot, err := parseSnapshotQuery(c, snapshotQueryOptions{allowAPI: true})
	if err != nil {
		return normalizedSpecQuery{}, err
	}

	format := formatJSON
	if hasQueryParam(c, "format") {
		rawFormat := strings.TrimSpace(c.Query("format"))
		if rawFormat == "" {
			return normalizedSpecQuery{}, invalidQuery("format must not be empty")
		}

		parsedFormat, ok := parseContentFormat(rawFormat)
		if !ok {
			return normalizedSpecQuery{}, invalidQuery("format must be json or yaml")
		}
		format = parsedFormat
	}

	return normalizedSpecQuery{
		Snapshot: snapshot,
		Format:   format,
	}, nil
}

func parseOperationEndpointQuery(c *fiber.Ctx) (store.ResolveReadSnapshotInput, normalizedOperationSelector, error) {
	if err := rejectUnsupportedQueryParams(c, "format"); err != nil {
		return store.ResolveReadSnapshotInput{}, normalizedOperationSelector{}, err
	}

	snapshot, err := parseSnapshotQuery(c, snapshotQueryOptions{allowAPI: true})
	if err != nil {
		return store.ResolveReadSnapshotInput{}, normalizedOperationSelector{}, err
	}

	selector, err := parseOperationSelectorQuery(c)
	if err != nil {
		return store.ResolveReadSnapshotInput{}, normalizedOperationSelector{}, err
	}

	return snapshot, selector, nil
}

func parseAPIsQuery(c *fiber.Ctx) (store.ResolveReadSnapshotInput, error) {
	if err := rejectUnsupportedQueryParams(c, "api", "operation_id", "method", "path", "format"); err != nil {
		return store.ResolveReadSnapshotInput{}, err
	}
	return parseSnapshotQuery(c, snapshotQueryOptions{})
}

func parseOperationsQuery(c *fiber.Ctx) (store.ResolveReadSnapshotInput, error) {
	if err := rejectUnsupportedQueryParams(c, "operation_id", "method", "path", "format"); err != nil {
		return store.ResolveReadSnapshotInput{}, err
	}
	return parseSnapshotQuery(c, snapshotQueryOptions{allowAPI: true})
}

func parseCatalogStatusQuery(c *fiber.Ctx) (string, error) {
	if err := rejectUnsupportedQueryParams(c, "api", "revision_id", "sha", "operation_id", "method", "path", "format"); err != nil {
		return "", err
	}

	repoPath := strings.TrimSpace(c.Query("repo"))
	if repoPath == "" {
		return "", invalidQuery("repo must not be empty")
	}
	return repoPath, nil
}

func parseReposQuery(c *fiber.Ctx) error {
	return rejectUnsupportedQueryParams(c, "repo", "api", "revision_id", "sha", "operation_id", "method", "path", "format")
}

type snapshotQueryOptions struct {
	allowAPI bool
}

func parseSnapshotQuery(c *fiber.Ctx, options snapshotQueryOptions) (store.ResolveReadSnapshotInput, error) {
	repoPath := strings.TrimSpace(c.Query("repo"))
	if repoPath == "" {
		return store.ResolveReadSnapshotInput{}, invalidQuery("repo must not be empty")
	}

	var apiPath string
	if hasQueryParam(c, "api") {
		if !options.allowAPI {
			return store.ResolveReadSnapshotInput{}, invalidQuery("api is not supported for this endpoint")
		}

		apiPath = strings.TrimSpace(c.Query("api"))
		if apiPath == "" {
			return store.ResolveReadSnapshotInput{}, invalidQuery("api must not be empty")
		}
	}

	revisionID, err := parseOptionalPositiveInt64Query(c, "revision_id")
	if err != nil {
		return store.ResolveReadSnapshotInput{}, err
	}

	sha := strings.TrimSpace(c.Query("sha"))
	if hasQueryParam(c, "sha") && sha == "" {
		return store.ResolveReadSnapshotInput{}, invalidQuery("sha must not be empty")
	}
	if revisionID > 0 && sha != "" {
		return store.ResolveReadSnapshotInput{}, invalidQuery("revision_id and sha are mutually exclusive")
	}
	if sha != "" && !isShortSHA(sha) {
		return store.ResolveReadSnapshotInput{}, invalidQuery("sha must be exactly 8 lowercase hex characters")
	}

	return store.ResolveReadSnapshotInput{
		RepoPath:   repoPath,
		APIPath:    apiPath,
		RevisionID: revisionID,
		SHA:        sha,
	}, nil
}

func parseOperationSelectorQuery(c *fiber.Ctx) (normalizedOperationSelector, error) {
	operationIDPresent := hasQueryParam(c, "operation_id")
	methodPresent := hasQueryParam(c, "method")
	pathPresent := hasQueryParam(c, "path")

	if operationIDPresent {
		operationID := strings.TrimSpace(c.Query("operation_id"))
		if operationID == "" {
			return normalizedOperationSelector{}, invalidQuery("operation_id must not be empty")
		}
		if methodPresent || pathPresent {
			return normalizedOperationSelector{}, invalidQuery("operation_id is mutually exclusive with method and path")
		}
		return normalizedOperationSelector{OperationID: operationID}, nil
	}

	if !methodPresent && !pathPresent {
		return normalizedOperationSelector{}, invalidQuery("either operation_id or method and path are required")
	}
	if !methodPresent || !pathPresent {
		return normalizedOperationSelector{}, invalidQuery("method and path must be provided together")
	}

	method := normalizeHTTPMethod(c.Query("method"))
	if method == "" {
		return normalizedOperationSelector{}, invalidQuery("method must be one of get, post, put, patch, delete, head, options, trace")
	}

	path := normalizeLookupPath(c.Query("path"))
	if path == "" {
		return normalizedOperationSelector{}, invalidQuery("path must not be empty")
	}

	return normalizedOperationSelector{
		Method: method,
		Path:   path,
	}, nil
}

func parseOptionalPositiveInt64Query(c *fiber.Ctx, name string) (int64, error) {
	if !hasQueryParam(c, name) {
		return 0, nil
	}

	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return 0, invalidQuery(fmt.Sprintf("%s must not be empty", name))
	}

	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, invalidQuery(fmt.Sprintf("%s must be a positive integer", name))
	}
	if value < 1 {
		return 0, invalidQuery(fmt.Sprintf("%s must be a positive integer", name))
	}

	return value, nil
}

func rejectUnsupportedQueryParams(c *fiber.Ctx, names ...string) error {
	for _, name := range names {
		if hasQueryParam(c, name) {
			return invalidQuery(fmt.Sprintf("%s is not supported for this endpoint", name))
		}
	}
	return nil
}

func hasQueryParam(c *fiber.Ctx, name string) bool {
	return c.Request().URI().QueryArgs().Has(name)
}

func normalizeLookupPath(value string) string {
	path := strings.TrimSpace(value)
	if path == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

func invalidQuery(message string) error {
	return &queryValidationError{message: message}
}

func isShortSHA(value string) bool {
	if len(value) != 8 {
		return false
	}
	for _, character := range value {
		switch {
		case character >= '0' && character <= '9':
			continue
		case character >= 'a' && character <= 'f':
			continue
		default:
			return false
		}
	}
	return true
}

var errOperationNotFound = errors.New("operation not found")
var errSpecNotFound = errors.New("spec not found")
var errAPISnapshotNotFound = errors.New("api snapshot not found")
