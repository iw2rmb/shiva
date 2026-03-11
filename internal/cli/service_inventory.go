package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/iw2rmb/shiva/internal/cli/catalog"
	clioutput "github.com/iw2rmb/shiva/internal/cli/output"
	"github.com/iw2rmb/shiva/internal/cli/request"
)

type SyncResult struct {
	Repo                  string                   `json:"repo"`
	Scope                 string                   `json:"scope"`
	SnapshotRevision      *clioutput.RevisionState `json:"snapshot_revision,omitempty"`
	APICount              int                      `json:"api_count"`
	OperationCatalogCount int                      `json:"operation_catalog_count"`
	APIs                  []string                 `json:"apis,omitempty"`
}

func (s *RuntimeService) ListRepos(
	ctx context.Context,
	options RequestOptions,
	format clioutput.ListFormat,
) ([]byte, error) {
	if s == nil || s.catalogService == nil || s.catalogStore == nil || s.newClient == nil {
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

	if err := s.catalogService.PrepareRepos(ctx, client, source.Name, catalogRefreshOptions(options)); err != nil {
		return nil, normalizeServiceError(err)
	}

	record, found, err := s.catalogStore.LoadRepos(source.Name)
	if err != nil {
		return nil, normalizeServiceError(err)
	}
	if !found {
		return nil, &NotFoundError{Message: fmt.Sprintf("repo catalog for profile %q is not cached", source.Name)}
	}

	var rows []clioutput.RepoRow
	if err := json.Unmarshal(record.Payload, &rows); err != nil {
		return nil, fmt.Errorf("decode repo inventory: %w", err)
	}
	return clioutput.RenderRepos(rows, format)
}

func (s *RuntimeService) ListAPIs(
	ctx context.Context,
	selector request.Envelope,
	options RequestOptions,
	format clioutput.ListFormat,
) ([]byte, error) {
	if s == nil || s.catalogService == nil || s.catalogStore == nil || s.newClient == nil {
		return nil, fmt.Errorf("CLI service is not configured")
	}

	normalized, err := normalizeInventorySelector(selector, false)
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

	prepared, err := s.catalogService.PrepareAPIs(ctx, client, source.Name, normalized, catalogRefreshOptions(options))
	if err != nil {
		return nil, normalizeServiceError(err)
	}

	record, found, err := s.catalogStore.LoadAPIs(source.Name, normalized.Repo, prepared.Scope)
	if err != nil {
		return nil, normalizeServiceError(err)
	}
	if !found {
		return nil, &NotFoundError{Message: fmt.Sprintf("api catalog for repo %q is not cached", normalized.Repo)}
	}

	var rows []clioutput.APIRow
	if err := json.Unmarshal(record.Payload, &rows); err != nil {
		return nil, fmt.Errorf("decode api inventory: %w", err)
	}
	for index := range rows {
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
	if s == nil || s.catalogService == nil || s.catalogStore == nil || s.newClient == nil {
		return nil, fmt.Errorf("CLI service is not configured")
	}

	normalized, err := normalizeInventorySelector(selector, true)
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

	prepared, err := s.catalogService.PrepareOperations(ctx, client, source.Name, normalized, catalogRefreshOptions(options))
	if err != nil {
		return nil, normalizeServiceError(err)
	}

	record, found, err := s.catalogStore.LoadOperations(source.Name, normalized.Repo, normalized.API, prepared.Scope)
	if err != nil {
		return nil, normalizeServiceError(err)
	}
	if !found {
		return nil, &NotFoundError{Message: fmt.Sprintf("operation catalog for repo %q is not cached", normalized.Repo)}
	}

	var rows []clioutput.OperationRow
	if err := json.Unmarshal(record.Payload, &rows); err != nil {
		return nil, fmt.Errorf("decode operation inventory: %w", err)
	}
	for index := range rows {
		rows[index].Repo = normalized.Repo
	}

	return clioutput.RenderOperations(rows, format)
}

func (s *RuntimeService) Sync(
	ctx context.Context,
	selector request.Envelope,
	options RequestOptions,
) ([]byte, error) {
	if s == nil || s.catalogService == nil || s.catalogStore == nil || s.newClient == nil {
		return nil, fmt.Errorf("CLI service is not configured")
	}

	normalized, err := normalizeInventorySelector(selector, false)
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

	refreshOptions := catalogRefreshOptions(options)
	refreshOptions.Refresh = true

	prepared, err := s.catalogService.PrepareAPIs(ctx, client, source.Name, normalized, refreshOptions)
	if err != nil {
		return nil, normalizeServiceError(err)
	}

	apiRecord, found, err := s.catalogStore.LoadAPIs(source.Name, normalized.Repo, prepared.Scope)
	if err != nil {
		return nil, normalizeServiceError(err)
	}
	if !found {
		return nil, &NotFoundError{Message: fmt.Sprintf("api catalog for repo %q is not cached", normalized.Repo)}
	}

	var apiRows []clioutput.APIRow
	if err := json.Unmarshal(apiRecord.Payload, &apiRows); err != nil {
		return nil, fmt.Errorf("decode synced api inventory: %w", err)
	}

	if _, err := s.catalogService.PrepareOperations(ctx, client, source.Name, normalized, refreshOptions); err != nil {
		return nil, normalizeServiceError(err)
	}

	apiNames := make([]string, 0, len(apiRows))
	operationCatalogCount := 1
	for _, row := range apiRows {
		apiNames = append(apiNames, row.API)
		if !row.HasSnapshot {
			continue
		}

		apiSelector := normalized
		apiSelector.API = row.API
		if _, err := s.catalogService.PrepareOperations(ctx, client, source.Name, apiSelector, refreshOptions); err != nil {
			return nil, normalizeServiceError(err)
		}
		operationCatalogCount++
	}

	body, err := json.Marshal(SyncResult{
		Repo:                  normalized.Repo,
		Scope:                 prepared.Scope.Key,
		SnapshotRevision:      revisionStateFromFingerprint(prepared.Fingerprint),
		APICount:              len(apiRows),
		OperationCatalogCount: operationCatalogCount,
		APIs:                  apiNames,
	})
	if err != nil {
		return nil, fmt.Errorf("encode sync result: %w", err)
	}
	return body, nil
}

func normalizeInventorySelector(selector request.Envelope, allowAPI bool) (request.Envelope, error) {
	repo, api, revisionID, sha, err := request.NormalizeSnapshotSelector(
		selector.Repo,
		selector.API,
		selector.RevisionID,
		selector.SHA,
	)
	if err != nil {
		return request.Envelope{}, normalizeCLIValidation(err)
	}
	if !allowAPI && strings.TrimSpace(api) != "" {
		return request.Envelope{}, &InvalidInputError{Message: "this command does not accept --api"}
	}

	return request.Envelope{
		Repo:       repo,
		API:        api,
		RevisionID: revisionID,
		SHA:        sha,
	}, nil
}

func revisionStateFromFingerprint(fingerprint catalog.SnapshotFingerprint) *clioutput.RevisionState {
	if fingerprint.RevisionID < 1 && strings.TrimSpace(fingerprint.SHA) == "" {
		return nil
	}

	return &clioutput.RevisionState{
		ID:  fingerprint.RevisionID,
		SHA: strings.TrimSpace(fingerprint.SHA),
	}
}

func catalogRefreshOptions(options RequestOptions) catalog.RefreshOptions {
	return catalog.RefreshOptions{
		Refresh: options.Refresh,
		Offline: options.Offline,
	}
}
