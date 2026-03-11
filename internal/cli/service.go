package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/iw2rmb/shiva/internal/openapi"
)

type Service interface {
	GetRepoSpec(ctx context.Context, repoPath string) ([]byte, error)
	GetOperation(ctx context.Context, repoPath string, operationID string) ([]byte, error)
}

type specClient interface {
	ListAPISpecs(ctx context.Context, repoPath string) ([]APISpecListing, error)
	GetSpec(ctx context.Context, repoPath string, apiRoot string, format SpecFormat) ([]byte, error)
}

type DraftService struct {
	client specClient
}

func NewService(client *HTTPClient) *DraftService {
	return &DraftService{client: client}
}

func (s *DraftService) GetRepoSpec(ctx context.Context, repoPath string) ([]byte, error) {
	apiRoot, err := s.resolveSingleActiveAPI(ctx, repoPath)
	if err != nil {
		return nil, err
	}

	return s.client.GetSpec(ctx, repoPath, apiRoot, SpecFormatYAML)
}

func (s *DraftService) GetOperation(ctx context.Context, repoPath string, operationID string) ([]byte, error) {
	apiRoot, err := s.resolveSingleActiveAPI(ctx, repoPath)
	if err != nil {
		return nil, err
	}

	specJSON, err := s.client.GetSpec(ctx, repoPath, apiRoot, SpecFormatJSON)
	if err != nil {
		return nil, err
	}

	return operationPayloadByID(repoPath, operationID, specJSON)
}

func (s *DraftService) resolveSingleActiveAPI(ctx context.Context, repoPath string) (string, error) {
	if s == nil || s.client == nil {
		return "", fmt.Errorf("draft cli service is not configured")
	}

	listings, err := s.client.ListAPISpecs(ctx, repoPath)
	if err != nil {
		return "", err
	}

	activeAPIs := make([]string, 0, len(listings))
	for _, listing := range listings {
		if strings.EqualFold(strings.TrimSpace(listing.Status), "active") {
			activeAPIs = append(activeAPIs, listing.API)
		}
	}

	switch len(activeAPIs) {
	case 0:
		return "", &NotFoundError{
			Message: fmt.Sprintf("repo %q has no active api specs", repoPath),
		}
	case 1:
		return activeAPIs[0], nil
	default:
		return "", &AmbiguousAPIError{
			Repo: repoPath,
			APIs: activeAPIs,
		}
	}
}

func operationPayloadByID(repoPath string, operationID string, specJSON []byte) ([]byte, error) {
	endpoints, err := openapi.ExtractEndpointsFromSpecJSON(specJSON)
	if err != nil {
		return nil, fmt.Errorf("extract endpoints from canonical spec: %w", err)
	}

	matches := make([]openapi.Endpoint, 0, 1)
	for _, endpoint := range endpoints {
		if endpoint.OperationID == operationID {
			matches = append(matches, endpoint)
		}
	}

	switch len(matches) {
	case 0:
		return nil, &NotFoundError{
			Message: fmt.Sprintf("operation %q was not found in repo %q", operationID, repoPath),
		}
	case 1:
		payload, err := buildOperationSlicePayload(matches[0])
		if err != nil {
			return nil, err
		}
		return json.Marshal(payload)
	default:
		candidates := make([]OperationCandidate, 0, len(matches))
		for _, match := range matches {
			candidates = append(candidates, OperationCandidate{
				Method: match.Method,
				Path:   match.Path,
			})
		}
		return nil, &AmbiguousOperationError{
			Repo:        repoPath,
			OperationID: operationID,
			Candidates:  candidates,
		}
	}
}

func buildOperationSlicePayload(endpoint openapi.Endpoint) (map[string]any, error) {
	if len(endpoint.RawJSON) == 0 {
		return nil, fmt.Errorf("operation payload is empty for %s %s", endpoint.Method, endpoint.Path)
	}

	var operation any
	if err := json.Unmarshal(endpoint.RawJSON, &operation); err != nil {
		return nil, fmt.Errorf("decode operation payload: %w", err)
	}

	return map[string]any{
		"paths": map[string]any{
			endpoint.Path: map[string]any{
				endpoint.Method: operation,
			},
		},
	}, nil
}
