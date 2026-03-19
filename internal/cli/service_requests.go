package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	clioutput "github.com/iw2rmb/shiva/internal/cli/output"
	"github.com/iw2rmb/shiva/internal/cli/request"
)

func (s *RuntimeService) EmitRepoRequests(ctx context.Context, options RequestOptions) ([]byte, error) {
	if s == nil || s.newClient == nil {
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

	body, err := client.ListRepos(ctx)
	if err != nil {
		return nil, normalizeServiceError(err)
	}

	var rows []clioutput.RepoRow
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("decode repo inventory: %w", err)
	}

	envelopes := make([]request.Envelope, 0, len(rows))
	for _, row := range rows {
		if row.SnapshotRevision == nil || row.ActiveAPICount != 1 {
			continue
		}
		envelopes = append(envelopes, request.Envelope{
			Kind:       request.KindSpec,
			Namespace:  row.Namespace,
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
	if s == nil || s.newClient == nil {
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

	body, err := client.ListAPIs(ctx, normalized)
	if err != nil {
		return nil, normalizeServiceError(err)
	}

	var rows []clioutput.APIRow
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("decode api inventory: %w", err)
	}

	envelopes := make([]request.Envelope, 0, len(rows))
	for _, row := range rows {
		if !row.HasSnapshot {
			continue
		}
		envelopes = append(envelopes, request.Envelope{
			Kind:       request.KindSpec,
			Namespace:  normalized.Namespace,
			Repo:       normalized.Repo,
			API:        row.API,
			RevisionID: chooseRevisionID(row.IngestEventID, normalized.RevisionID),
			SHA:        chooseSHA(row.IngestEventSHA, normalized.SHA),
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
	if s == nil || s.newClient == nil {
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

	body, err := client.ListOperations(ctx, normalized)
	if err != nil {
		return nil, normalizeServiceError(err)
	}

	var rows []clioutput.OperationRow
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("decode operation inventory: %w", err)
	}

	envelopes := make([]request.Envelope, 0, len(rows))
	for _, row := range rows {
		envelope := request.Envelope{
			Kind:       request.KindOperation,
			Namespace:  normalized.Namespace,
			Repo:       normalized.Repo,
			API:        row.API,
			RevisionID: chooseRevisionID(row.IngestEventID, normalized.RevisionID),
			SHA:        chooseSHA(row.IngestEventSHA, normalized.SHA),
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
