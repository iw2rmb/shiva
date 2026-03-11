package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/iw2rmb/shiva/internal/cli/request"
	"gopkg.in/yaml.v3"
)

type Service interface {
	GetSpec(ctx context.Context, selector request.Envelope, format SpecFormat) ([]byte, error)
	GetOperation(ctx context.Context, selector request.Envelope) ([]byte, error)
	PlanCall(ctx context.Context, selector request.Envelope) ([]byte, error)
	Health(ctx context.Context) ([]byte, error)
}

type queryClient interface {
	GetSpec(ctx context.Context, selector request.Envelope, format SpecFormat) ([]byte, error)
	GetOperation(ctx context.Context, selector request.Envelope) ([]byte, error)
	PlanCall(ctx context.Context, selector request.Envelope) ([]byte, error)
	Health(ctx context.Context) ([]byte, error)
}

type DraftService struct {
	client queryClient
}

func NewService(client *HTTPClient) *DraftService {
	return &DraftService{client: client}
}

func (s *DraftService) GetSpec(ctx context.Context, selector request.Envelope, format SpecFormat) ([]byte, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("draft cli service is not configured")
	}

	normalized, err := request.NormalizeEnvelope(selector, request.NormalizeOptions{
		DefaultKind:      request.KindSpec,
		AllowMissingKind: true,
	})
	if err != nil {
		return nil, normalizeCLIValidation(err)
	}

	return s.client.GetSpec(ctx, normalized, format)
}

func (s *DraftService) GetOperation(ctx context.Context, selector request.Envelope) ([]byte, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("draft cli service is not configured")
	}

	normalized, err := request.NormalizeEnvelope(selector, request.NormalizeOptions{
		DefaultKind:      request.KindOperation,
		AllowMissingKind: true,
	})
	if err != nil {
		return nil, normalizeCLIValidation(err)
	}

	return s.client.GetOperation(ctx, normalized)
}

func (s *DraftService) PlanCall(ctx context.Context, selector request.Envelope) ([]byte, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("draft cli service is not configured")
	}

	normalized, err := request.NormalizeCallEnvelope(selector, request.NormalizeCallOptions{
		DefaultTarget:    strings.TrimSpace(selector.Target),
		AllowMissingKind: true,
	})
	if err != nil {
		return nil, normalizeCLIValidation(err)
	}

	return s.client.PlanCall(ctx, normalized)
}

func (s *DraftService) Health(ctx context.Context) ([]byte, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("draft cli service is not configured")
	}
	return s.client.Health(ctx)
}

func ConvertJSONToYAML(body []byte) ([]byte, error) {
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("decode json output: %w", err)
	}

	yamlBody, err := yaml.Marshal(decoded)
	if err != nil {
		return nil, fmt.Errorf("encode yaml output: %w", err)
	}
	return yamlBody, nil
}
