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

func (s *RuntimeService) EmitRepoRequests(ctx context.Context, options RequestOptions) ([]byte, error) {
	if s == nil || s.catalogService == nil || s.catalogStore == nil || s.newClient == nil {
		return nil, fmt.Errorf("CLI service is not configured")
	}

	source, _, err := s.resolveSourceAndTarget(options.Profile, "")
	if err != nil {
		return nil, err
	}

	client, err := s.newTransportClient(source)
	if err != nil {
		return nil, err
	}

	if err := s.catalogService.PrepareRepos(ctx, client, source.Name, s.refreshOptions("repos", source.Name, request.Envelope{}, options)); err != nil {
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

	envelopes := make([]request.Envelope, 0, len(rows))
	for _, row := range rows {
		if row.SnapshotRevision == nil || row.ActiveAPICount != 1 {
			continue
		}
		envelopes = append(envelopes, request.Envelope{
			Kind:       request.KindSpec,
			Repo:       row.Repo,
			RevisionID: row.SnapshotRevision.ID,
			SHA:        strings.TrimSpace(row.SnapshotRevision.SHA),
		})
	}
	return clioutput.RenderRequestEnvelopesNDJSON(envelopes)
}

func (s *RuntimeService) EmitAPIRequests(
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

	source, _, err := s.resolveSourceAndTarget(options.Profile, "")
	if err != nil {
		return nil, err
	}

	client, err := s.newTransportClient(source)
	if err != nil {
		return nil, err
	}

	prepared, err := s.catalogService.PrepareAPIs(ctx, client, source.Name, normalized, s.refreshOptions("apis", source.Name, normalized, options))
	if err != nil {
		return nil, normalizeServiceError(err)
	}

	rows, err := s.loadAPIRows(source.Name, normalized.Repo, prepared.Scope)
	if err != nil {
		return nil, err
	}

	envelopes := make([]request.Envelope, 0, len(rows))
	for _, row := range rows {
		if !row.HasSnapshot {
			continue
		}
		envelopes = append(envelopes, request.Envelope{
			Kind:       request.KindSpec,
			Repo:       normalized.Repo,
			API:        row.API,
			RevisionID: chooseRevisionID(row.IngestEventID, prepared.Fingerprint.RevisionID, normalized.RevisionID),
			SHA:        chooseSHA(row.IngestEventSHA, prepared.Fingerprint.SHA, normalized.SHA),
		})
	}
	return clioutput.RenderRequestEnvelopesNDJSON(envelopes)
}

func (s *RuntimeService) EmitOperationRequests(
	ctx context.Context,
	selector request.Envelope,
	options RequestOptions,
	targetName string,
) ([]byte, error) {
	if s == nil || s.catalogService == nil || s.catalogStore == nil || s.newClient == nil {
		return nil, fmt.Errorf("CLI service is not configured")
	}

	normalized, err := normalizeInventorySelector(selector, true)
	if err != nil {
		return nil, err
	}

	source, _, err := s.resolveSourceAndTarget(options.Profile, targetName)
	if err != nil {
		return nil, err
	}

	client, err := s.newTransportClient(source)
	if err != nil {
		return nil, err
	}

	prepared, err := s.catalogService.PrepareOperations(ctx, client, source.Name, normalized, s.refreshOptions("ops", source.Name, normalized, options))
	if err != nil {
		return nil, normalizeServiceError(err)
	}

	rows, err := s.loadOperationInventoryRows(source.Name, normalized, prepared.Scope)
	if err != nil {
		return nil, err
	}

	envelopes := make([]request.Envelope, 0, len(rows))
	for _, row := range rows {
		envelope := request.Envelope{
			Kind:       request.KindOperation,
			Repo:       normalized.Repo,
			API:        row.API,
			RevisionID: chooseRevisionID(row.IngestEventID, prepared.Fingerprint.RevisionID, normalized.RevisionID),
			SHA:        chooseSHA(row.IngestEventSHA, prepared.Fingerprint.SHA, normalized.SHA),
		}
		if strings.TrimSpace(targetName) != "" {
			envelope.Kind = request.KindCall
			envelope.Target = strings.TrimSpace(targetName)
		}
		if strings.TrimSpace(row.OperationID) != "" {
			envelope.OperationID = row.OperationID
		} else {
			envelope.Method = row.Method
			envelope.Path = row.Path
		}
		envelopes = append(envelopes, envelope)
	}

	return clioutput.RenderRequestEnvelopesNDJSON(envelopes)
}

func (s *RuntimeService) loadAPIRows(profileName string, repo string, scope catalog.Scope) ([]clioutput.APIRow, error) {
	record, found, err := s.catalogStore.LoadAPIs(profileName, repo, scope)
	if err != nil {
		return nil, normalizeServiceError(err)
	}
	if !found {
		return nil, &NotFoundError{Message: fmt.Sprintf("api catalog for repo %q is not cached", repo)}
	}

	var rows []clioutput.APIRow
	if err := json.Unmarshal(record.Payload, &rows); err != nil {
		return nil, fmt.Errorf("decode api inventory: %w", err)
	}
	return rows, nil
}
