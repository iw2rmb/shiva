package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/iw2rmb/shiva/internal/cli/catalog"
	"github.com/iw2rmb/shiva/internal/cli/executor"
	clioutput "github.com/iw2rmb/shiva/internal/cli/output"
	"github.com/iw2rmb/shiva/internal/cli/profile"
	"github.com/iw2rmb/shiva/internal/cli/request"
	"github.com/iw2rmb/shiva/internal/cli/target"
)

func (s *RuntimeService) ExecuteCall(
	ctx context.Context,
	selector request.Envelope,
	options RequestOptions,
	format CallFormat,
) ([]byte, error) {
	if s == nil || s.catalogService == nil || s.catalogStore == nil || s.newClient == nil {
		return nil, fmt.Errorf("CLI service is not configured")
	}

	normalized, err := request.NormalizeCallEnvelope(selector, request.NormalizeCallOptions{
		DefaultTarget:    request.DefaultShivaTarget,
		AllowMissingKind: true,
	})
	if err != nil {
		return nil, normalizeCLIValidation(err)
	}

	source, resolvedTarget, err := s.resolveSourceAndTarget(options.Profile, normalized.Target)
	if err != nil {
		return nil, err
	}
	if resolvedTarget == nil {
		return nil, &InvalidInputError{Message: "call target is not configured"}
	}

	client, err := s.newTransportClient(source)
	if err != nil {
		return nil, err
	}

	prepared, err := s.catalogService.PrepareCall(ctx, client, source.Name, normalized, catalogOptions(options))
	if err != nil {
		return nil, normalizeServiceError(err)
	}

	resolvedEnvelope, err := s.resolvePreparedCall(source.Name, normalized, prepared)
	if err != nil {
		return nil, err
	}

	plan, err := buildCallPlan(resolvedEnvelope, source, *resolvedTarget)
	if err != nil {
		return nil, normalizeServiceError(err)
	}

	if plan.Dispatch.DryRun {
		return renderDryRun(plan, format)
	}

	response, err := executor.Execute(ctx, plan)
	if err != nil {
		return nil, normalizeServiceError(err)
	}

	switch format {
	case CallFormatBody:
		return clioutput.RenderCallBody(response), nil
	case CallFormatJSON:
		return clioutput.RenderCallResultJSON(plan, response)
	default:
		return nil, &InvalidInputError{Message: fmt.Sprintf("unsupported call output %q", format)}
	}
}

func buildCallPlan(envelope request.Envelope, source profile.Source, targetEntry target.Entry) (executor.CallPlan, error) {
	switch targetEntry.Mode {
	case target.ModeShiva:
		return executor.PlanShivaDispatchCall(envelope, source.BaseURL, source.ResolvedToken(), source.Timeout)
	case target.ModeDirect:
		return executor.PlanDirectCall(envelope, targetEntry.BaseURL, targetEntry.ResolvedToken(), targetEntry.Timeout)
	default:
		return executor.CallPlan{}, fmt.Errorf("unsupported target mode %q", targetEntry.Mode)
	}
}

func renderDryRun(plan executor.CallPlan, format CallFormat) ([]byte, error) {
	switch format {
	case CallFormatJSON:
		return clioutput.RenderCallPlanJSON(plan)
	case CallFormatCurl:
		return clioutput.RenderCallCurl(plan)
	default:
		return nil, &InvalidInputError{Message: fmt.Sprintf("unsupported dry-run output %q", format)}
	}
}

func (s *RuntimeService) resolvePreparedCall(
	profileName string,
	selector request.Envelope,
	prepared catalog.PreparedSnapshot,
) (request.Envelope, error) {
	rows, err := s.loadOperationInventoryRows(profileName, selector, prepared.Scope)
	if err != nil {
		return request.Envelope{}, err
	}

	candidates := filterOperationRows(rows, selector)
	switch len(candidates) {
	case 0:
		return request.Envelope{}, &NotFoundError{
			Message: fmt.Sprintf("operation selector did not match repo %q", selector.Repo),
		}
	case 1:
	default:
		ambiguity := make([]OperationCandidate, 0, len(candidates))
		for _, candidate := range candidates {
			ambiguity = append(ambiguity, OperationCandidate{
				API:         candidate.API,
				Method:      candidate.Method,
				Path:        candidate.Path,
				OperationID: candidate.OperationID,
			})
		}
		return request.Envelope{}, &AmbiguousOperationError{
			Message:    "operation selector matched multiple operations",
			Candidates: ambiguity,
		}
	}

	row := candidates[0]
	resolved := selector
	resolved.Kind = request.KindCall
	resolved.API = row.API
	resolved.RevisionID = chooseRevisionID(row.IngestEventID, prepared.Fingerprint.RevisionID, selector.RevisionID)
	resolved.SHA = chooseSHA(row.IngestEventSHA, prepared.Fingerprint.SHA, selector.SHA)
	if strings.TrimSpace(row.OperationID) != "" {
		resolved.OperationID = row.OperationID
	}
	resolved.Method = row.Method
	resolved.Path = row.Path

	normalized, err := request.NormalizeResolvedCallEnvelope(resolved, selector.Target)
	if err != nil {
		return request.Envelope{}, normalizeCLIValidation(err)
	}
	return normalized, nil
}

func filterOperationRows(rows []clioutput.OperationRow, selector request.Envelope) []clioutput.OperationRow {
	candidates := make([]clioutput.OperationRow, 0, len(rows))
	for _, row := range rows {
		switch {
		case strings.TrimSpace(selector.OperationID) != "":
			if row.OperationID == selector.OperationID {
				candidates = append(candidates, row)
			}
		case row.Method == selector.Method && row.Path == selector.Path:
			candidates = append(candidates, row)
		}
	}
	return candidates
}

func chooseRevisionID(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func chooseSHA(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (s *RuntimeService) loadOperationInventoryRows(
	profileName string,
	selector request.Envelope,
	scope catalog.Scope,
) ([]clioutput.OperationRow, error) {
	record, found, err := s.catalogStore.LoadOperations(profileName, selector.RepoPath(), selector.API, scope)
	if err != nil {
		return nil, normalizeServiceError(err)
	}
	if !found {
		return nil, &NotFoundError{Message: fmt.Sprintf("operation catalog for repo %q is not cached", selector.RepoPath())}
	}

	var rows []clioutput.OperationRow
	if err := json.Unmarshal(record.Payload, &rows); err != nil {
		return nil, fmt.Errorf("decode operation inventory: %w", err)
	}
	return rows, nil
}
