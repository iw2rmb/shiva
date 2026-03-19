package httpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/iw2rmb/shiva/internal/cli/request"
	"github.com/iw2rmb/shiva/internal/store"
)

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

type apiSnapshotResponse struct {
	API               string `json:"api"`
	Status            string `json:"status"`
	DisplayName       string `json:"display_name,omitempty"`
	HasSnapshot       bool   `json:"has_snapshot"`
	APISpecRevisionID int64  `json:"api_spec_revision_id,omitempty"`
	IngestEventID     int64  `json:"ingest_event_id,omitempty"`
	IngestEventSHA    string `json:"ingest_event_sha,omitempty"`
	IngestEventBranch string `json:"ingest_event_branch,omitempty"`
	SpecETag          string `json:"spec_etag,omitempty"`
	SpecSizeBytes     int64  `json:"spec_size_bytes,omitempty"`
	OperationCount    int64  `json:"operation_count"`
}

type operationSnapshotResponse struct {
	API               string          `json:"api"`
	Status            string          `json:"status"`
	APISpecRevisionID int64           `json:"api_spec_revision_id"`
	IngestEventID     int64           `json:"ingest_event_id"`
	IngestEventSHA    string          `json:"ingest_event_sha"`
	IngestEventBranch string          `json:"ingest_event_branch"`
	Method            string          `json:"method"`
	Path              string          `json:"path"`
	OperationID       string          `json:"operation_id,omitempty"`
	Summary           string          `json:"summary,omitempty"`
	Deprecated        bool            `json:"deprecated"`
	Operation         json.RawMessage `json:"operation,omitempty"`
}

type repoCatalogResponse struct {
	Namespace          string                        `json:"namespace"`
	Repo               string                        `json:"repo"`
	GitLabProjectID    int64                         `json:"gitlab_project_id"`
	DefaultBranch      string                        `json:"default_branch"`
	OpenAPIForceRescan bool                          `json:"openapi_force_rescan"`
	ActiveAPICount     int64                         `json:"active_api_count"`
	HeadRevision       *catalogRevisionStateResponse `json:"head_revision,omitempty"`
	SnapshotRevision   *catalogRevisionStateResponse `json:"snapshot_revision,omitempty"`
}

type namespaceCatalogResponse struct {
	Namespace  string `json:"namespace"`
	RepoCount  int64  `json:"repo_count"`
	AllPending bool   `json:"all_pending"`
}

type catalogRevisionStateResponse struct {
	ID             int64      `json:"id"`
	SHA            string     `json:"sha"`
	Status         string     `json:"status"`
	OpenAPIChanged *bool      `json:"openapi_changed,omitempty"`
	ReceivedAt     *time.Time `json:"received_at,omitempty"`
	ProcessedAt    *time.Time `json:"processed_at,omitempty"`
}

func normalizeHTTPMethod(value string) string {
	method := strings.ToLower(strings.TrimSpace(value))
	if _, ok := supportedHTTPMethods[method]; !ok {
		return ""
	}
	return method
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

func rawJSONObject(raw []byte) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, errors.New("operation raw json must not be empty")
	}
	if trimmed[0] != '{' {
		return nil, errors.New("operation raw json must be an object")
	}
	if !json.Valid(trimmed) {
		return nil, errors.New("operation raw json must be valid json")
	}
	return append(json.RawMessage(nil), trimmed...), nil
}

func writeSpecArtifactResponse(c *fiber.Ctx, artifact store.SpecArtifact, format contentFormat) error {
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
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "unsupported format",
		})
	}
}

func writeRawOperationResponse(c *fiber.Ctx, raw []byte) error {
	operation, err := rawJSONObject(raw)
	if err != nil {
		return err
	}

	c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSONCharsetUTF8)
	return c.Status(fiber.StatusOK).Send(operation)
}

func writeAPIAmbiguity(c *fiber.Ctx, message string, candidates []store.APISnapshot) error {
	return c.Status(fiber.StatusConflict).JSON(fiber.Map{
		"error":      message,
		"candidates": mapAPISnapshots(candidates),
	})
}

func writeOperationAmbiguity(c *fiber.Ctx, message string, candidates []store.OperationSnapshot) error {
	response, err := mapOperationSnapshots(candidates, false)
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusConflict).JSON(fiber.Map{
		"error":      message,
		"candidates": response,
	})
}

func mapAPISnapshots(items []store.APISnapshot) []apiSnapshotResponse {
	response := make([]apiSnapshotResponse, 0, len(items))
	for _, item := range items {
		response = append(response, apiSnapshotResponse{
			API:               item.API,
			Status:            item.Status,
			DisplayName:       item.DisplayName,
			HasSnapshot:       item.HasSnapshot,
			APISpecRevisionID: item.APISpecRevisionID,
			IngestEventID:     item.IngestEventID,
			IngestEventSHA:    item.IngestEventSHA,
			IngestEventBranch: item.IngestEventBranch,
			SpecETag:          item.SpecETag,
			SpecSizeBytes:     item.SpecSizeBytes,
			OperationCount:    item.OperationCount,
		})
	}
	return response
}

func mapOperationSnapshots(items []store.OperationSnapshot, includeOperation bool) ([]operationSnapshotResponse, error) {
	response := make([]operationSnapshotResponse, 0, len(items))
	for _, item := range items {
		row := operationSnapshotResponse{
			API:               item.API,
			Status:            item.Status,
			APISpecRevisionID: item.APISpecRevisionID,
			IngestEventID:     item.IngestEventID,
			IngestEventSHA:    item.IngestEventSHA,
			IngestEventBranch: item.IngestEventBranch,
			Method:            item.Method,
			Path:              item.Path,
			OperationID:       item.OperationID,
			Summary:           item.Summary,
			Deprecated:        item.Deprecated,
		}
		if includeOperation {
			operation, err := rawJSONObject(item.RawJSON)
			if err != nil {
				return nil, fmt.Errorf("map operation snapshot %s %s: %w", item.Method, item.Path, err)
			}
			row.Operation = operation
		}
		response = append(response, row)
	}
	return response, nil
}

func mapRepoCatalogEntries(items []store.RepoCatalogEntry) []repoCatalogResponse {
	response := make([]repoCatalogResponse, 0, len(items))
	for _, item := range items {
		response = append(response, mapRepoCatalogEntry(item))
	}
	return response
}

func mapNamespaceCatalogEntries(items []store.NamespaceCatalogEntry) []namespaceCatalogResponse {
	response := make([]namespaceCatalogResponse, 0, len(items))
	for _, item := range items {
		response = append(response, namespaceCatalogResponse{
			Namespace:  item.Namespace,
			RepoCount:  item.RepoCount,
			AllPending: item.AllPending,
		})
	}
	return response
}

func mapRepoCatalogEntry(item store.RepoCatalogEntry) repoCatalogResponse {
	return repoCatalogResponse{
		Namespace:          item.Repo.Namespace,
		Repo:               item.Repo.Repo,
		GitLabProjectID:    item.Repo.GitLabProjectID,
		DefaultBranch:      item.Repo.DefaultBranch,
		OpenAPIForceRescan: item.OpenAPIForceRescan,
		ActiveAPICount:     item.ActiveAPICount,
		HeadRevision:       mapCatalogRevisionState(item.HeadRevision),
		SnapshotRevision:   mapCatalogRevisionState(item.SnapshotRevision),
	}
}

func mapCatalogRevisionState(item *store.CatalogRevisionState) *catalogRevisionStateResponse {
	if item == nil {
		return nil
	}

	return &catalogRevisionStateResponse{
		ID:             item.ID,
		SHA:            item.SHA,
		Status:         item.Status,
		OpenAPIChanged: item.OpenAPIChanged,
		ReceivedAt:     item.ReceivedAt,
		ProcessedAt:    item.ProcessedAt,
	}
}

func (s *Server) writeQueryError(c *fiber.Ctx, err error) error {
	var validationErr *queryValidationError
	var requestValidationErr *request.ValidationError

	switch {
	case errors.As(err, &validationErr):
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	case errors.As(err, &requestValidationErr):
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	case errors.Is(err, store.ErrStoreNotConfigured):
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "database is not configured",
		})
	case errors.Is(err, store.ErrReadSnapshotInvalidInput):
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	case errors.Is(err, store.ErrReadSnapshotUnprocessed):
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{
			"error": err.Error(),
		})
	case errors.Is(err, store.ErrReadSnapshotNotFound),
		errors.Is(err, store.ErrRepoNotFound),
		errors.Is(err, store.ErrSpecArtifactNotFound),
		errors.Is(err, errSpecNotFound),
		errors.Is(err, errAPISnapshotNotFound),
		errors.Is(err, errOperationNotFound):
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": err.Error(),
		})
	default:
		if s.logger != nil {
			s.logger.Error("query route failed", "error", err)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "internal server error",
		})
	}
}
