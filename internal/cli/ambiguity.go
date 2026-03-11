package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/iw2rmb/shiva/internal/cli/httpclient"
)

type ambiguityPayload[T any] struct {
	Error      string `json:"error"`
	Candidates []T    `json:"candidates"`
}

type apiCandidatePayload struct {
	API    string `json:"api"`
	Status string `json:"status"`
}

type operationCandidatePayload struct {
	API         string `json:"api"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	OperationID string `json:"operation_id,omitempty"`
}

func normalizeHTTPConflict(err *httpclient.HTTPError) error {
	if err == nil || err.StatusCode != 409 || len(err.Body) == 0 {
		return nil
	}

	var operationPayload ambiguityPayload[operationCandidatePayload]
	if decodeAmbiguityPayload(err.Body, &operationPayload) && len(operationPayload.Candidates) > 0 {
		if hasOperationCandidateData(operationPayload.Candidates) {
			candidates := make([]OperationCandidate, 0, len(operationPayload.Candidates))
			for _, candidate := range operationPayload.Candidates {
				candidates = append(candidates, OperationCandidate{
					API:         candidate.API,
					Method:      candidate.Method,
					Path:        candidate.Path,
					OperationID: candidate.OperationID,
				})
			}
			return &AmbiguousOperationError{
				Message:    strings.TrimSpace(operationPayload.Error),
				Candidates: candidates,
			}
		}
	}

	var apiPayload ambiguityPayload[apiCandidatePayload]
	if decodeAmbiguityPayload(err.Body, &apiPayload) && len(apiPayload.Candidates) > 0 {
		candidates := make([]APICandidate, 0, len(apiPayload.Candidates))
		for _, candidate := range apiPayload.Candidates {
			candidates = append(candidates, APICandidate{
				API:    candidate.API,
				Status: candidate.Status,
			})
		}
		return &AmbiguousAPIError{
			Message:    strings.TrimSpace(apiPayload.Error),
			Candidates: candidates,
		}
	}

	return nil
}

func decodeAmbiguityPayload[T any](body []byte, output *ambiguityPayload[T]) bool {
	if output == nil {
		return false
	}
	if err := json.Unmarshal(body, output); err != nil {
		return false
	}
	return strings.TrimSpace(output.Error) != "" || len(output.Candidates) > 0
}

func hasOperationCandidateData(candidates []operationCandidatePayload) bool {
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.Method) != "" || strings.TrimSpace(candidate.Path) != "" || strings.TrimSpace(candidate.OperationID) != "" {
			return true
		}
	}
	return false
}

func formatCandidates[T fmt.Stringer](candidates []T) string {
	lines := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		lines = append(lines, "  - "+candidate.String())
	}
	return strings.Join(lines, "\n")
}
