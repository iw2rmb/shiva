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
	if err != nil {
		return nil, normalizeServiceError(err)
	}
	if err := json.Unmarshal(body, &rows); err != nil {
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
	if s == nil || s.newClient == nil {
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

	body, err := client.ListOperations(ctx, normalized)
	if err != nil {
		return nil, normalizeServiceError(err)
	}

	var rows []clioutput.OperationRow
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("decode operation inventory: %w", err)
	}
	for index := range rows {
		rows[index].Namespace = normalized.Namespace
		rows[index].Repo = normalized.Repo
	}

	return clioutput.RenderOperations(rows, format)
}

func (s *RuntimeService) Sync(
	ctx context.Context,
	selector request.Envelope,
	options RequestOptions,
) ([]byte, error) {
	if s == nil || s.newClient == nil {
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

func normalizeInventorySelector(selector request.Envelope, allowAPI bool) (request.Envelope, error) {
	namespace, repo, api, revisionID, sha, err := request.NormalizeSnapshotSelector(
		selector.Namespace,
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
