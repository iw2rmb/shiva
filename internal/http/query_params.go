package httpserver

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/iw2rmb/shiva/internal/cli/request"
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
	Envelope request.Envelope
	Format   contentFormat
}

type namespacesCatalogQuery struct {
	QueryPrefix string
	Limit       int32
	Offset      int32
}

type namespacesCatalogCountQuery struct {
	QueryPrefix string
}

type reposCatalogCountQuery struct {
	Namespace string
	Query     string
}

type reposCatalogQuery struct {
	Namespace string
	Query     string
	Limit     int32
	Offset    int32
}

type operationsCatalogCountQuery struct {
	Namespace string
	Repo      string
	Query     string
}

type apisCatalogQuery struct {
	Snapshot store.ResolveReadSnapshotInput
	Query    string
	Limit    int32
	Offset   int32
}

type apisCatalogCountQuery struct {
	Snapshot store.ResolveReadSnapshotInput
	Query    string
}

type operationsCatalogQuery struct {
	Snapshot store.ResolveReadSnapshotInput
	Query    string
	Limit    int32
	Offset   int32
}

const (
	defaultNamespacesPageLimit int32 = 100
	maxNamespacesPageLimit     int32 = 1000
	defaultReposPageLimit      int32 = 100
	maxReposPageLimit          int32 = 1000
	defaultAPIsPageLimit       int32 = 200
	maxAPIsPageLimit           int32 = 2000
	defaultOperationsPageLimit int32 = 200
	maxOperationsPageLimit     int32 = 2000
)

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

	envelope, err := request.NormalizeEnvelope(request.Envelope{
		Kind:       request.KindSpec,
		Namespace:  snapshot.Namespace,
		Repo:       snapshot.Repo,
		API:        snapshot.APIPath,
		RevisionID: snapshot.RevisionID,
		SHA:        snapshot.SHA,
	}, request.NormalizeOptions{
		DefaultKind: request.KindSpec,
	})
	if err != nil {
		return normalizedSpecQuery{}, err
	}

	return normalizedSpecQuery{
		Envelope: envelope,
		Format:   format,
	}, nil
}

func parseOperationEndpointQuery(c *fiber.Ctx) (request.Envelope, error) {
	if err := rejectUnsupportedQueryParams(c, "format"); err != nil {
		return request.Envelope{}, err
	}

	snapshot, err := parseSnapshotQuery(c, snapshotQueryOptions{allowAPI: true})
	if err != nil {
		return request.Envelope{}, err
	}

	envelope, err := request.NormalizeEnvelope(request.Envelope{
		Kind:        request.KindOperation,
		Namespace:   snapshot.Namespace,
		Repo:        snapshot.Repo,
		API:         snapshot.APIPath,
		RevisionID:  snapshot.RevisionID,
		SHA:         snapshot.SHA,
		OperationID: strings.TrimSpace(c.Query("operation_id")),
		Method:      c.Query("method"),
		Path:        c.Query("path"),
	}, request.NormalizeOptions{
		DefaultKind: request.KindOperation,
	})
	if err != nil {
		return request.Envelope{}, err
	}

	return envelope, nil
}

func parseAPIsQuery(c *fiber.Ctx) (apisCatalogQuery, error) {
	if err := rejectUnsupportedQueryParams(c, "api", "operation_id", "method", "path", "format"); err != nil {
		return apisCatalogQuery{}, err
	}

	namespace := strings.TrimSpace(c.Query("namespace"))
	repo := strings.TrimSpace(c.Query("repo"))
	queryPrefix := strings.TrimSpace(c.Query("query"))
	if hasQueryParam(c, "namespace") && namespace == "" {
		return apisCatalogQuery{}, invalidQuery("namespace must not be empty")
	}
	if hasQueryParam(c, "repo") && repo == "" {
		return apisCatalogQuery{}, invalidQuery("repo must not be empty")
	}
	if repo != "" && namespace == "" {
		return apisCatalogQuery{}, invalidQuery("namespace is required when repo is provided")
	}
	if hasQueryParam(c, "query") && queryPrefix == "" {
		return apisCatalogQuery{}, invalidQuery("query must not be empty")
	}

	revisionID, err := parseOptionalPositiveInt64Query(c, "revision_id")
	if err != nil {
		return apisCatalogQuery{}, err
	}
	sha := strings.TrimSpace(c.Query("sha"))
	if hasQueryParam(c, "sha") && sha == "" {
		return apisCatalogQuery{}, invalidQuery("sha must not be empty")
	}
	if revisionID > 0 && sha != "" {
		return apisCatalogQuery{}, invalidQuery("revision_id and sha are mutually exclusive")
	}
	if sha != "" && !request.IsShortSHA(sha) {
		return apisCatalogQuery{}, invalidQuery("sha must be exactly 8 lowercase hex characters")
	}
	if (revisionID > 0 || sha != "") && (namespace == "" || repo == "") {
		return apisCatalogQuery{}, invalidQuery("namespace and repo are required when revision_id or sha are provided")
	}

	limit, offset, err := parsePaginationQuery(c, defaultAPIsPageLimit, maxAPIsPageLimit)
	if err != nil {
		return apisCatalogQuery{}, err
	}

	return apisCatalogQuery{
		Snapshot: store.ResolveReadSnapshotInput{
			Namespace:  namespace,
			Repo:       repo,
			RevisionID: revisionID,
			SHA:        sha,
		},
		Query:  queryPrefix,
		Limit:  limit,
		Offset: offset,
	}, nil
}

func parseOperationsQuery(c *fiber.Ctx) (operationsCatalogQuery, error) {
	if err := rejectUnsupportedQueryParams(c, "operation_id", "method", "path", "format"); err != nil {
		return operationsCatalogQuery{}, err
	}

	namespace := strings.TrimSpace(c.Query("namespace"))
	repo := strings.TrimSpace(c.Query("repo"))
	api := strings.TrimSpace(c.Query("api"))
	queryPrefix := strings.TrimSpace(c.Query("query"))
	if hasQueryParam(c, "query") && queryPrefix == "" {
		return operationsCatalogQuery{}, invalidQuery("query must not be empty")
	}

	revisionID, err := parseOptionalPositiveInt64Query(c, "revision_id")
	if err != nil {
		return operationsCatalogQuery{}, err
	}
	sha := strings.TrimSpace(c.Query("sha"))
	if hasQueryParam(c, "sha") && sha == "" {
		return operationsCatalogQuery{}, invalidQuery("sha must not be empty")
	}
	if revisionID > 0 && sha != "" {
		return operationsCatalogQuery{}, invalidQuery("revision_id and sha are mutually exclusive")
	}
	if sha != "" && !request.IsShortSHA(sha) {
		return operationsCatalogQuery{}, invalidQuery("sha must be exactly 8 lowercase hex characters")
	}

	if repo != "" && namespace == "" {
		return operationsCatalogQuery{}, invalidQuery("namespace is required when repo is provided")
	}
	if (api != "" || revisionID > 0 || sha != "") && (namespace == "" || repo == "") {
		return operationsCatalogQuery{}, invalidQuery("namespace and repo are required when api, revision_id, or sha are provided")
	}

	limit, offset, err := parsePaginationQuery(c, defaultOperationsPageLimit, maxOperationsPageLimit)
	if err != nil {
		return operationsCatalogQuery{}, err
	}

	return operationsCatalogQuery{
		Snapshot: store.ResolveReadSnapshotInput{
			Namespace:  namespace,
			Repo:       repo,
			APIPath:    api,
			RevisionID: revisionID,
			SHA:        sha,
		},
		Query:  queryPrefix,
		Limit:  limit,
		Offset: offset,
	}, nil
}

func parseCatalogStatusQuery(c *fiber.Ctx) (string, error) {
	if err := rejectUnsupportedQueryParams(c, "api", "revision_id", "sha", "operation_id", "method", "path", "format"); err != nil {
		return "", err
	}

	namespace := strings.TrimSpace(c.Query("namespace"))
	repo := strings.TrimSpace(c.Query("repo"))
	if namespace == "" {
		return "", invalidQuery("namespace must not be empty")
	}
	if repo == "" {
		return "", invalidQuery("repo must not be empty")
	}
	return namespace + "/" + repo, nil
}

func parseReposQuery(c *fiber.Ctx) (reposCatalogQuery, error) {
	if err := rejectUnsupportedQueryParams(c, "repo", "api", "revision_id", "sha", "operation_id", "method", "path", "format"); err != nil {
		return reposCatalogQuery{}, err
	}

	namespace := strings.TrimSpace(c.Query("namespace"))
	queryPrefix := strings.TrimSpace(c.Query("query"))
	if hasQueryParam(c, "query") && queryPrefix == "" {
		return reposCatalogQuery{}, invalidQuery("query must not be empty")
	}
	limit, offset, err := parsePaginationQuery(c, defaultReposPageLimit, maxReposPageLimit)
	if err != nil {
		return reposCatalogQuery{}, err
	}
	return reposCatalogQuery{
		Namespace: namespace,
		Query:     queryPrefix,
		Limit:     limit,
		Offset:    offset,
	}, nil
}

func parseNamespacesQuery(c *fiber.Ctx) (namespacesCatalogQuery, error) {
	if err := rejectUnsupportedQueryParams(c, "namespace", "repo", "api", "revision_id", "sha", "operation_id", "method", "path", "format"); err != nil {
		return namespacesCatalogQuery{}, err
	}

	queryPrefix := strings.TrimSpace(c.Query("query"))
	if hasQueryParam(c, "query") && queryPrefix == "" {
		return namespacesCatalogQuery{}, invalidQuery("query must not be empty")
	}

	limit := defaultNamespacesPageLimit
	if hasQueryParam(c, "limit") {
		value, err := parseRequiredPositiveInt32Query(c, "limit")
		if err != nil {
			return namespacesCatalogQuery{}, err
		}
		if value > maxNamespacesPageLimit {
			return namespacesCatalogQuery{}, invalidQuery(fmt.Sprintf("limit must be <= %d", maxNamespacesPageLimit))
		}
		limit = value
	}

	offset := int32(0)
	if hasQueryParam(c, "offset") {
		value, err := parseRequiredNonNegativeInt32Query(c, "offset")
		if err != nil {
			return namespacesCatalogQuery{}, err
		}
		offset = value
	}

	return namespacesCatalogQuery{
		QueryPrefix: queryPrefix,
		Limit:       limit,
		Offset:      offset,
	}, nil
}

func parseNamespacesCountQuery(c *fiber.Ctx) (namespacesCatalogCountQuery, error) {
	if err := rejectUnsupportedQueryParams(
		c,
		"namespace",
		"repo",
		"api",
		"revision_id",
		"sha",
		"operation_id",
		"method",
		"path",
		"format",
		"limit",
		"offset",
	); err != nil {
		return namespacesCatalogCountQuery{}, err
	}

	queryPrefix := strings.TrimSpace(c.Query("query"))
	if hasQueryParam(c, "query") && queryPrefix == "" {
		return namespacesCatalogCountQuery{}, invalidQuery("query must not be empty")
	}

	return namespacesCatalogCountQuery{QueryPrefix: queryPrefix}, nil
}

func parseReposCountQuery(c *fiber.Ctx) (reposCatalogCountQuery, error) {
	if err := rejectUnsupportedQueryParams(
		c,
		"repo",
		"api",
		"revision_id",
		"sha",
		"operation_id",
		"method",
		"path",
		"format",
		"limit",
		"offset",
	); err != nil {
		return reposCatalogCountQuery{}, err
	}

	namespace := strings.TrimSpace(c.Query("namespace"))
	if hasQueryParam(c, "namespace") && namespace == "" {
		return reposCatalogCountQuery{}, invalidQuery("namespace must not be empty")
	}

	queryPrefix := strings.TrimSpace(c.Query("query"))
	if hasQueryParam(c, "query") && queryPrefix == "" {
		return reposCatalogCountQuery{}, invalidQuery("query must not be empty")
	}

	return reposCatalogCountQuery{Namespace: namespace, Query: queryPrefix}, nil
}

func parseOperationsCountQuery(c *fiber.Ctx) (operationsCatalogCountQuery, error) {
	if err := rejectUnsupportedQueryParams(
		c,
		"api",
		"revision_id",
		"sha",
		"operation_id",
		"method",
		"path",
		"format",
		"limit",
		"offset",
	); err != nil {
		return operationsCatalogCountQuery{}, err
	}

	namespace := strings.TrimSpace(c.Query("namespace"))
	if hasQueryParam(c, "namespace") && namespace == "" {
		return operationsCatalogCountQuery{}, invalidQuery("namespace must not be empty")
	}

	repo := strings.TrimSpace(c.Query("repo"))
	if hasQueryParam(c, "repo") && repo == "" {
		return operationsCatalogCountQuery{}, invalidQuery("repo must not be empty")
	}
	if repo != "" && namespace == "" {
		return operationsCatalogCountQuery{}, invalidQuery("namespace is required when repo is provided")
	}
	queryPrefix := strings.TrimSpace(c.Query("query"))
	if hasQueryParam(c, "query") && queryPrefix == "" {
		return operationsCatalogCountQuery{}, invalidQuery("query must not be empty")
	}

	return operationsCatalogCountQuery{
		Namespace: namespace,
		Repo:      repo,
		Query:     queryPrefix,
	}, nil
}

func parseAPIsCountQuery(c *fiber.Ctx) (apisCatalogCountQuery, error) {
	if err := rejectUnsupportedQueryParams(
		c,
		"api",
		"operation_id",
		"method",
		"path",
		"format",
		"limit",
		"offset",
	); err != nil {
		return apisCatalogCountQuery{}, err
	}

	namespace := strings.TrimSpace(c.Query("namespace"))
	repo := strings.TrimSpace(c.Query("repo"))
	queryPrefix := strings.TrimSpace(c.Query("query"))
	if hasQueryParam(c, "namespace") && namespace == "" {
		return apisCatalogCountQuery{}, invalidQuery("namespace must not be empty")
	}
	if hasQueryParam(c, "repo") && repo == "" {
		return apisCatalogCountQuery{}, invalidQuery("repo must not be empty")
	}
	if repo != "" && namespace == "" {
		return apisCatalogCountQuery{}, invalidQuery("namespace is required when repo is provided")
	}
	if hasQueryParam(c, "query") && queryPrefix == "" {
		return apisCatalogCountQuery{}, invalidQuery("query must not be empty")
	}

	revisionID, err := parseOptionalPositiveInt64Query(c, "revision_id")
	if err != nil {
		return apisCatalogCountQuery{}, err
	}
	sha := strings.TrimSpace(c.Query("sha"))
	if hasQueryParam(c, "sha") && sha == "" {
		return apisCatalogCountQuery{}, invalidQuery("sha must not be empty")
	}
	if revisionID > 0 && sha != "" {
		return apisCatalogCountQuery{}, invalidQuery("revision_id and sha are mutually exclusive")
	}
	if sha != "" && !request.IsShortSHA(sha) {
		return apisCatalogCountQuery{}, invalidQuery("sha must be exactly 8 lowercase hex characters")
	}
	if (revisionID > 0 || sha != "") && (namespace == "" || repo == "") {
		return apisCatalogCountQuery{}, invalidQuery("namespace and repo are required when revision_id or sha are provided")
	}

	return apisCatalogCountQuery{
		Snapshot: store.ResolveReadSnapshotInput{
			Namespace:  namespace,
			Repo:       repo,
			RevisionID: revisionID,
			SHA:        sha,
		},
		Query: queryPrefix,
	}, nil
}

type snapshotQueryOptions struct {
	allowAPI bool
}

func parseSnapshotQuery(c *fiber.Ctx, options snapshotQueryOptions) (store.ResolveReadSnapshotInput, error) {
	namespace := strings.TrimSpace(c.Query("namespace"))
	if namespace == "" {
		return store.ResolveReadSnapshotInput{}, invalidQuery("namespace must not be empty")
	}

	repo := strings.TrimSpace(c.Query("repo"))
	if repo == "" {
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
	if sha != "" && !request.IsShortSHA(sha) {
		return store.ResolveReadSnapshotInput{}, invalidQuery("sha must be exactly 8 lowercase hex characters")
	}

	return store.ResolveReadSnapshotInput{
		Namespace:  namespace,
		Repo:       repo,
		APIPath:    apiPath,
		RevisionID: revisionID,
		SHA:        sha,
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

func parseRequiredPositiveInt32Query(c *fiber.Ctx, name string) (int32, error) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return 0, invalidQuery(fmt.Sprintf("%s must not be empty", name))
	}
	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || value < 1 {
		return 0, invalidQuery(fmt.Sprintf("%s must be a positive integer", name))
	}
	return int32(value), nil
}

func parseRequiredNonNegativeInt32Query(c *fiber.Ctx, name string) (int32, error) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return 0, invalidQuery(fmt.Sprintf("%s must not be empty", name))
	}
	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || value < 0 {
		return 0, invalidQuery(fmt.Sprintf("%s must be a non-negative integer", name))
	}
	return int32(value), nil
}

func parsePaginationQuery(c *fiber.Ctx, defaultLimit int32, maxLimit int32) (int32, int32, error) {
	limit := defaultLimit
	if hasQueryParam(c, "limit") {
		value, err := parseRequiredPositiveInt32Query(c, "limit")
		if err != nil {
			return 0, 0, err
		}
		if value > maxLimit {
			return 0, 0, invalidQuery(fmt.Sprintf("limit must be <= %d", maxLimit))
		}
		limit = value
	}

	offset := int32(0)
	if hasQueryParam(c, "offset") {
		value, err := parseRequiredNonNegativeInt32Query(c, "offset")
		if err != nil {
			return 0, 0, err
		}
		offset = value
	}
	return limit, offset, nil
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

func invalidQuery(message string) error {
	return &queryValidationError{message: message}
}

var errOperationNotFound = errors.New("operation not found")
var errSpecNotFound = errors.New("spec not found")
var errAPISnapshotNotFound = errors.New("api snapshot not found")
