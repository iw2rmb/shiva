package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
	if s == nil || s.newClient == nil {
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
	if options.Offline {
		return nil, &InvalidInputError{Message: "offline mode is not supported"}
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

	resolvedEnvelope, err := resolveCallFromOperationRows(normalized, rows)
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

func resolveCallFromOperationRows(selector request.Envelope, rows []clioutput.OperationRow) (request.Envelope, error) {
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
	resolved.RevisionID = chooseRevisionID(row.IngestEventID, selector.RevisionID)
	resolved.SHA = chooseSHA(row.IngestEventSHA, selector.SHA)
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
