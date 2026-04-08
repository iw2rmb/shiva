package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	clioutput "github.com/iw2rmb/shiva/internal/cli/output"
	"github.com/iw2rmb/shiva/internal/cli/request"
)

type SyncResult struct {
	Namespace             string                   `json:"namespace"`
	Repo                  string                   `json:"repo"`
	Scope                 string                   `json:"scope"`
	SnapshotRevision      *clioutput.RevisionState `json:"snapshot_revision,omitempty"`
	APICount              int                      `json:"api_count"`
	OperationCatalogCount int                      `json:"operation_catalog_count"`
	APIs                  []string                 `json:"apis,omitempty"`
}

type CatalogCount struct {
	TotalCount    int64 `json:"total_count"`
	MaxItemLength int64 `json:"max_item_length"`
}

func (s *RuntimeService) CountNamespaces(ctx context.Context, options RequestOptions) (int64, error) {
	if s == nil || s.newClient == nil {
		return 0, fmt.Errorf("CLI service is not configured")
	}

	source, err := s.resolveSource(options.Profile, "")
	if err != nil {
		return 0, err
	}

	client, err := s.newTransportClient(source)
	if err != nil {
		return 0, err
	}

	var payload struct {
		TotalCount int64 `json:"total_count"`
	}
	body, err := client.CountNamespaces(ctx)
	if err != nil {
		return 0, normalizeServiceError(err)
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return 0, fmt.Errorf("decode namespace count: %w", err)
	}
	return payload.TotalCount, nil
}

func (s *RuntimeService) CountNamespaceCatalog(ctx context.Context, options RequestOptions) (CatalogCount, error) {
	if s == nil || s.newClient == nil {
		return CatalogCount{}, fmt.Errorf("CLI service is not configured")
	}

	source, err := s.resolveSource(options.Profile, "")
	if err != nil {
		return CatalogCount{}, err
	}

	client, err := s.newTransportClient(source)
	if err != nil {
		return CatalogCount{}, err
	}

	var payload CatalogCount
	body, err := client.CountNamespaces(ctx)
	if strings.TrimSpace(options.Query) != "" {
		if filtered, ok := client.(filteredCountTransportClient); ok {
			body, err = filtered.CountNamespacesFiltered(ctx, options.Query)
		}
	}
	if err != nil {
		return CatalogCount{}, normalizeServiceError(err)
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return CatalogCount{}, fmt.Errorf("decode namespace count: %w", err)
	}
	return payload, nil
}

func (s *RuntimeService) CountRepoCatalog(
	ctx context.Context,
	namespace string,
	options RequestOptions,
) (CatalogCount, error) {
	if s == nil || s.newClient == nil {
		return CatalogCount{}, fmt.Errorf("CLI service is not configured")
	}

	source, err := s.resolveSource(options.Profile, "")
	if err != nil {
		return CatalogCount{}, err
	}

	client, err := s.newTransportClient(source)
	if err != nil {
		return CatalogCount{}, err
	}

	var payload CatalogCount
	body, err := client.CountRepos(ctx, strings.TrimSpace(namespace))
	if strings.TrimSpace(options.Query) != "" {
		if filtered, ok := client.(filteredCountTransportClient); ok {
			body, err = filtered.CountReposFiltered(ctx, strings.TrimSpace(namespace), options.Query)
		}
	}
	if err != nil {
		return CatalogCount{}, normalizeServiceError(err)
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return CatalogCount{}, fmt.Errorf("decode repo count: %w", err)
	}
	return payload, nil
}

func (s *RuntimeService) CountOperationCatalog(
	ctx context.Context,
	selector request.Envelope,
	options RequestOptions,
) (CatalogCount, error) {
	if s == nil || s.newClient == nil {
		return CatalogCount{}, fmt.Errorf("CLI service is not configured")
	}

	normalized, err := normalizeInventorySelector(selector, true, true)
	if err != nil {
		return CatalogCount{}, err
	}

	source, err := s.resolveSource(options.Profile, "")
	if err != nil {
		return CatalogCount{}, err
	}

	client, err := s.newTransportClient(source)
	if err != nil {
		return CatalogCount{}, err
	}

	var payload CatalogCount
	body, err := client.CountOperations(ctx, normalized)
	if strings.TrimSpace(options.Query) != "" {
		if filtered, ok := client.(filteredCountTransportClient); ok {
			body, err = filtered.CountOperationsFiltered(ctx, normalized, options.Query)
		}
	}
	if err != nil {
		return CatalogCount{}, normalizeServiceError(err)
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return CatalogCount{}, fmt.Errorf("decode operation count: %w", err)
	}
	return payload, nil
}

func (s *RuntimeService) ListNamespaces(
	ctx context.Context,
	options RequestOptions,
	format clioutput.ListFormat,
) ([]byte, error) {
	if s == nil || s.newClient == nil {
		return nil, fmt.Errorf("CLI service is not configured")
	}

	source, err := s.resolveSource(options.Profile, "")
	if err != nil {
		return nil, err
	}

	client, err := s.newTransportClient(source)
	if err != nil {
		return nil, err
	}

	var rows []clioutput.NamespaceRow
	body, err := client.ListNamespaces(ctx)
	query := strings.TrimSpace(options.Query)
	usedServerFiltering := false
	usedServerPaging := false
	if query != "" {
		if filtered, ok := client.(filteredPagedTransportClient); ok {
			body, err = filtered.ListNamespacesPageFiltered(ctx, query, options.Limit, options.Offset)
			usedServerFiltering = true
			usedServerPaging = true
		}
	} else if options.Limit > 0 || options.Offset > 0 {
		if paged, ok := client.(pagedTransportClient); ok {
			body, err = paged.ListNamespacesPage(ctx, options.Limit, options.Offset)
			usedServerPaging = true
		}
	}
	if err != nil {
		return nil, normalizeServiceError(err)
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("decode namespace inventory: %w", err)
	}
	if !usedServerFiltering {
		rows = filterNamespaceRows(rows, query)
	}
	if !usedServerPaging {
		rows = paginateRows(rows, options.Limit, options.Offset)
	}
	return clioutput.RenderNamespaces(rows, format)
}

func (s *RuntimeService) ListRepos(
	ctx context.Context,
	options RequestOptions,
	format clioutput.ListFormat,
) ([]byte, error) {
	if s == nil || s.newClient == nil {
		return nil, fmt.Errorf("CLI service is not configured")
	}

	source, err := s.resolveSource(options.Profile, "")
	if err != nil {
		return nil, err
	}

	client, err := s.newTransportClient(source)
	if err != nil {
		return nil, err
	}

	var rows []clioutput.RepoRow
	body, err := client.ListRepos(ctx)
	query := strings.TrimSpace(options.Query)
	namespace := strings.TrimSpace(options.Namespace)
	usedServerFiltering := false
	usedServerPaging := false
	if query != "" {
		if filtered, ok := client.(filteredPagedTransportClient); ok {
			body, err = filtered.ListReposPageFiltered(ctx, namespace, query, options.Limit, options.Offset)
			usedServerFiltering = true
			usedServerPaging = true
		}
	} else if options.Limit > 0 || options.Offset > 0 {
		if paged, ok := client.(pagedTransportClient); ok {
			body, err = paged.ListReposPage(ctx, namespace, options.Limit, options.Offset)
			usedServerPaging = true
		}
	}
	if err != nil {
		return nil, normalizeServiceError(err)
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("decode repo inventory: %w", err)
	}
	if !usedServerFiltering {
		rows = filterRepoRows(rows, query)
	}
	if !usedServerPaging {
		rows = paginateRows(rows, options.Limit, options.Offset)
	}
	return clioutput.RenderRepos(rows, format)
}

func (s *RuntimeService) ListAPIs(
	ctx context.Context,
	selector request.Envelope,
	options RequestOptions,
	format clioutput.ListFormat,
) ([]byte, error) {
	if s == nil || s.newClient == nil {
		return nil, fmt.Errorf("CLI service is not configured")
	}

	normalized, err := normalizeInventorySelector(selector, false, false)
	if err != nil {
		return nil, err
	}

	source, err := s.resolveSource(options.Profile, "")
	if err != nil {
		return nil, err
	}

	client, err := s.newTransportClient(source)
	if err != nil {
		return nil, err
	}

	body, err := client.ListAPIs(ctx, normalized)
	if err != nil {
		return nil, normalizeServiceError(err)
	}

	var rows []clioutput.APIRow
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("decode api inventory: %w", err)
	}
	for index := range rows {
		rows[index].Namespace = normalized.Namespace
		rows[index].Repo = normalized.Repo
	}

	return clioutput.RenderAPIs(rows, format)
}

func (s *RuntimeService) ListOperations(
	ctx context.Context,
	selector request.Envelope,
	options RequestOptions,
	format clioutput.ListFormat,
) ([]byte, error) {
	if s == nil || s.newClient == nil {
		return nil, fmt.Errorf("CLI service is not configured")
	}

	normalized, err := normalizeInventorySelector(selector, true, true)
	if err != nil {
		return nil, err
	}

	source, err := s.resolveSource(options.Profile, "")
	if err != nil {
		return nil, err
	}

	client, err := s.newTransportClient(source)
	if err != nil {
		return nil, err
	}

	var body []byte
	query := strings.TrimSpace(options.Query)
	usedServerFiltering := false
	usedServerPaging := false
	if query != "" {
		if filtered, ok := client.(filteredPagedTransportClient); ok {
			body, err = filtered.ListOperationsPageFiltered(ctx, normalized, query, options.Limit, options.Offset)
			usedServerFiltering = true
			usedServerPaging = true
		} else {
			body, err = client.ListOperations(ctx, normalized)
		}
	} else if options.Limit > 0 || options.Offset > 0 {
		if paged, ok := client.(pagedTransportClient); ok {
			body, err = paged.ListOperationsPage(ctx, normalized, options.Limit, options.Offset)
			usedServerPaging = true
		} else {
			body, err = client.ListOperations(ctx, normalized)
		}
	} else {
		body, err = client.ListOperations(ctx, normalized)
	}
	if err != nil {
		return nil, normalizeServiceError(err)
	}

	var rows []clioutput.OperationRow
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("decode operation inventory: %w", err)
	}
	if !usedServerFiltering {
		rows = filterOperationRowsByQuery(rows, query)
	}
	if !usedServerPaging {
		rows = paginateRows(rows, options.Limit, options.Offset)
	}
	for index := range rows {
		if normalized.Namespace != "" {
			rows[index].Namespace = normalized.Namespace
		}
		if normalized.Repo != "" {
			rows[index].Repo = normalized.Repo
		}
	}

	return clioutput.RenderOperations(rows, format)
}

func paginateRows[T any](rows []T, limit int32, offset int32) []T {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 && offset == 0 {
		return rows
	}
	if offset >= int32(len(rows)) {
		return []T{}
	}
	start := int(offset)
	if limit <= 0 {
		return rows[start:]
	}
	end := start + int(limit)
	if end > len(rows) {
		end = len(rows)
	}
	return rows[start:end]
}

func filterNamespaceRows(rows []clioutput.NamespaceRow, query string) []clioutput.NamespaceRow {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return rows
	}
	filtered := make([]clioutput.NamespaceRow, 0, len(rows))
	for _, row := range rows {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(row.Namespace)), query) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func filterRepoRows(rows []clioutput.RepoRow, query string) []clioutput.RepoRow {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return rows
	}
	filtered := make([]clioutput.RepoRow, 0, len(rows))
	for _, row := range rows {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(row.Repo)), query) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func filterOperationRowsByQuery(rows []clioutput.OperationRow, query string) []clioutput.OperationRow {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return rows
	}
	filtered := make([]clioutput.OperationRow, 0, len(rows))
	for _, row := range rows {
		label := strings.ToLower(strings.TrimSpace(strings.ToUpper(strings.TrimSpace(row.Method)) + " " + strings.TrimSpace(row.Path)))
		operationID := strings.ToLower(strings.TrimSpace(row.OperationID))
		if strings.HasPrefix(label, query) || strings.HasPrefix(operationID, query) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func (s *RuntimeService) Sync(
	ctx context.Context,
	selector request.Envelope,
	options RequestOptions,
) ([]byte, error) {
	if s == nil || s.newClient == nil {
		return nil, fmt.Errorf("CLI service is not configured")
	}

	normalized, err := normalizeInventorySelector(selector, false, false)
	if err != nil {
		return nil, err
	}
	if normalized.API != "" {
		return nil, &InvalidInputError{Message: "sync does not accept --api; it refreshes the whole repo snapshot"}
	}
	if options.Offline {
		return nil, &InvalidInputError{Message: "sync does not support --offline"}
	}

	source, err := s.resolveSource(options.Profile, "")
	if err != nil {
		return nil, err
	}

	client, err := s.newTransportClient(source)
	if err != nil {
		return nil, err
	}

	var apiRows []clioutput.APIRow
	apiBody, err := client.ListAPIs(ctx, normalized)
	if err != nil {
		return nil, normalizeServiceError(err)
	}
	if err := json.Unmarshal(apiBody, &apiRows); err != nil {
		return nil, fmt.Errorf("decode synced api inventory: %w", err)
	}

	apiNames := make([]string, 0, len(apiRows))
	operationCatalogCount := 0
	for _, row := range apiRows {
		apiNames = append(apiNames, row.API)
		if !row.HasSnapshot {
			continue
		}

		apiSelector := normalized
		apiSelector.API = row.API
		if _, err := client.ListOperations(ctx, apiSelector); err != nil {
			return nil, normalizeServiceError(err)
		}
		operationCatalogCount++
	}

	body, err := json.Marshal(SyncResult{
		Namespace:             normalized.Namespace,
		Repo:                  normalized.Repo,
		Scope:                 scopeKeyFromSelector(normalized),
		SnapshotRevision:      revisionStateFromSelector(normalized),
		APICount:              len(apiRows),
		OperationCatalogCount: operationCatalogCount,
		APIs:                  apiNames,
	})
	if err != nil {
		return nil, fmt.Errorf("encode sync result: %w", err)
	}
	return body, nil
}

func normalizeInventorySelector(selector request.Envelope, allowAPI bool, allowUnscoped bool) (request.Envelope, error) {
	namespace := strings.TrimSpace(selector.Namespace)
	repo := strings.TrimSpace(selector.Repo)
	api := strings.TrimSpace(selector.API)
	revisionID := selector.RevisionID
	sha := strings.TrimSpace(selector.SHA)

	if allowUnscoped && namespace == "" && repo == "" && api == "" && revisionID == 0 && sha == "" {
		return request.Envelope{}, nil
	}
	if allowUnscoped {
		if repo != "" && namespace == "" {
			return request.Envelope{}, &InvalidInputError{Message: "namespace is required when repo is provided"}
		}
		if (api != "" || revisionID > 0 || sha != "") && (namespace == "" || repo == "") {
			return request.Envelope{}, &InvalidInputError{Message: "namespace and repo are required when api, revision_id, or sha are provided"}
		}
	}

	namespace, repo, api, revisionID, sha, err := request.NormalizeSnapshotSelector(namespace, repo, api, revisionID, sha)
	if err != nil {
		return request.Envelope{}, normalizeCLIValidation(err)
	}
	if !allowAPI && strings.TrimSpace(api) != "" {
		return request.Envelope{}, &InvalidInputError{Message: "this command does not accept --api"}
	}

	return request.Envelope{
		Namespace:  namespace,
		Repo:       repo,
		API:        api,
		RevisionID: revisionID,
		SHA:        sha,
	}, nil
}

func revisionStateFromSelector(selector request.Envelope) *clioutput.RevisionState {
	if selector.RevisionID < 1 && strings.TrimSpace(selector.SHA) == "" {
		return nil
	}

	return &clioutput.RevisionState{
		ID:  selector.RevisionID,
		SHA: strings.TrimSpace(selector.SHA),
	}
}

func scopeKeyFromSelector(selector request.Envelope) string {
	if selector.RevisionID > 0 {
		return fmt.Sprintf("rev:%d", selector.RevisionID)
	}
	if strings.TrimSpace(selector.SHA) != "" {
		return "sha:" + strings.TrimSpace(selector.SHA)
	}
	return "floating"
}
