package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/iw2rmb/shiva/internal/cli/catalog"
	"github.com/iw2rmb/shiva/internal/cli/config"
	"github.com/iw2rmb/shiva/internal/cli/httpclient"
	"github.com/iw2rmb/shiva/internal/cli/profile"
	"github.com/iw2rmb/shiva/internal/cli/request"
	"gopkg.in/yaml.v3"
)

type RequestOptions struct {
	Profile string
	Refresh bool
	Offline bool
}

type Service interface {
	GetSpec(ctx context.Context, selector request.Envelope, options RequestOptions, format SpecFormat) ([]byte, error)
	GetOperation(ctx context.Context, selector request.Envelope, options RequestOptions) ([]byte, error)
	PlanCall(ctx context.Context, selector request.Envelope, options RequestOptions) ([]byte, error)
	Health(ctx context.Context, options RequestOptions) ([]byte, error)
}

type transportClient interface {
	GetSpec(ctx context.Context, selector request.Envelope, format SpecFormat) ([]byte, error)
	GetOperation(ctx context.Context, selector request.Envelope) ([]byte, error)
	PlanCall(ctx context.Context, selector request.Envelope) ([]byte, error)
	ListRepos(ctx context.Context) ([]byte, error)
	GetCatalogStatus(ctx context.Context, repo string) ([]byte, error)
	ListAPIs(ctx context.Context, selector request.Envelope) ([]byte, error)
	ListOperations(ctx context.Context, selector request.Envelope) ([]byte, error)
	Health(ctx context.Context) ([]byte, error)
}

type RuntimeService struct {
	document       config.Document
	catalogService *catalog.Service
	catalogStore   *catalog.Store
	newClient      func(source profile.Source) (transportClient, error)
}

func NewService(document config.Document, catalogStore *catalog.Store) *RuntimeService {
	return &RuntimeService{
		document:       document,
		catalogService: catalog.NewService(catalogStore),
		catalogStore:   catalogStore,
		newClient: func(source profile.Source) (transportClient, error) {
			client, err := httpclient.New(httpclient.Config{
				BaseURL:        source.BaseURL,
				RequestTimeout: source.Timeout,
				Token:          source.ResolvedToken(),
			})
			if err != nil {
				return nil, err
			}
			return client, nil
		},
	}
}

func (s *RuntimeService) GetSpec(
	ctx context.Context,
	selector request.Envelope,
	options RequestOptions,
	format SpecFormat,
) ([]byte, error) {
	if s == nil || s.catalogService == nil || s.catalogStore == nil || s.newClient == nil {
		return nil, fmt.Errorf("CLI service is not configured")
	}

	normalized, err := request.NormalizeEnvelope(selector, request.NormalizeOptions{
		DefaultKind:      request.KindSpec,
		AllowMissingKind: true,
	})
	if err != nil {
		return nil, normalizeCLIValidation(err)
	}

	scope := catalog.ScopeFromSelector(normalized.RevisionID, normalized.SHA)
	source, err := s.resolveSource(options.Profile, normalized.Target)
	if err != nil {
		return nil, err
	}

	if cached, found, err := s.loadCachedSpecRecord(source.Name, normalized, scope, format); err != nil {
		return nil, normalizeServiceError(err)
	} else if found && options.Offline {
		return cached.Payload, nil
	}
	if options.Offline {
		return nil, &NotFoundError{Message: fmt.Sprintf("offline cache miss: spec for repo %q", normalized.Repo)}
	}

	client, err := s.newTransportClient(source)
	if err != nil {
		return nil, err
	}

	prepared, err := s.catalogService.PrepareSpec(ctx, client, source.Name, normalized, catalog.RefreshOptions{
		Refresh: options.Refresh,
		Offline: options.Offline,
	})
	if err != nil {
		if !options.Refresh {
			if cached, found, cacheErr := s.loadCachedSpecRecord(source.Name, normalized, scope, format); cacheErr == nil && found {
				return cached.Payload, nil
			}
		}
		return nil, normalizeServiceError(err)
	}

	pinnedSelector := pinSelectorToPreparedSnapshot(normalized, prepared)
	body, err := client.GetSpec(ctx, pinnedSelector, format)
	if err != nil {
		if !options.Refresh {
			if cached, found, cacheErr := s.loadCachedSpecRecord(source.Name, normalized, prepared.Scope, format); cacheErr == nil && found && cacheRecordMatches(cached, prepared.Fingerprint) {
				return cached.Payload, nil
			}
		}
		return nil, normalizeServiceError(err)
	}

	if err := s.catalogStore.SaveSpec(
		source.Name,
		normalized.Repo,
		normalized.API,
		prepared.Scope,
		string(format),
		body,
		prepared.Fingerprint,
	); err != nil {
		return nil, normalizeServiceError(err)
	}
	return body, nil
}

func (s *RuntimeService) GetOperation(
	ctx context.Context,
	selector request.Envelope,
	options RequestOptions,
) ([]byte, error) {
	if s == nil || s.catalogService == nil || s.catalogStore == nil || s.newClient == nil {
		return nil, fmt.Errorf("CLI service is not configured")
	}

	normalized, err := request.NormalizeEnvelope(selector, request.NormalizeOptions{
		DefaultKind:      request.KindOperation,
		AllowMissingKind: true,
	})
	if err != nil {
		return nil, normalizeCLIValidation(err)
	}

	scope := catalog.ScopeFromSelector(normalized.RevisionID, normalized.SHA)
	source, err := s.resolveSource(options.Profile, normalized.Target)
	if err != nil {
		return nil, err
	}

	selectorKey, err := operationSelectorKey(normalized)
	if err != nil {
		return nil, err
	}

	if cached, found, err := s.loadCachedOperationRecord(source.Name, normalized, scope, selectorKey); err != nil {
		return nil, normalizeServiceError(err)
	} else if found && options.Offline {
		return cached.Payload, nil
	}
	if options.Offline {
		return nil, &NotFoundError{Message: fmt.Sprintf("offline cache miss: operation for repo %q", normalized.Repo)}
	}

	client, err := s.newTransportClient(source)
	if err != nil {
		return nil, err
	}

	prepared, err := s.catalogService.PrepareOperation(ctx, client, source.Name, normalized, catalog.RefreshOptions{
		Refresh: options.Refresh,
		Offline: options.Offline,
	})
	if err != nil {
		if !options.Refresh {
			if cached, found, cacheErr := s.loadCachedOperationRecord(source.Name, normalized, scope, selectorKey); cacheErr == nil && found {
				return cached.Payload, nil
			}
		}
		return nil, normalizeServiceError(err)
	}

	pinnedSelector := pinSelectorToPreparedSnapshot(normalized, prepared)
	body, err := client.GetOperation(ctx, pinnedSelector)
	if err != nil {
		if !options.Refresh {
			if cached, found, cacheErr := s.loadCachedOperationRecord(source.Name, normalized, prepared.Scope, selectorKey); cacheErr == nil && found && cacheRecordMatches(cached, prepared.Fingerprint) {
				return cached.Payload, nil
			}
		}
		return nil, normalizeServiceError(err)
	}

	if err := s.catalogStore.SaveOperationResponse(
		source.Name,
		normalized.Repo,
		normalized.API,
		prepared.Scope,
		selectorKey,
		body,
		prepared.Fingerprint,
	); err != nil {
		return nil, normalizeServiceError(err)
	}
	return body, nil
}

func (s *RuntimeService) PlanCall(
	ctx context.Context,
	selector request.Envelope,
	options RequestOptions,
) ([]byte, error) {
	if s == nil || s.catalogService == nil || s.catalogStore == nil || s.newClient == nil {
		return nil, fmt.Errorf("CLI service is not configured")
	}

	normalized, err := request.NormalizeCallEnvelope(selector, request.NormalizeCallOptions{
		DefaultTarget:    strings.TrimSpace(selector.Target),
		AllowMissingKind: true,
	})
	if err != nil {
		return nil, normalizeCLIValidation(err)
	}

	scope := catalog.ScopeFromSelector(normalized.RevisionID, normalized.SHA)
	source, err := s.resolveSource(options.Profile, normalized.Target)
	if err != nil {
		return nil, err
	}

	selectorKey, err := envelopeCacheKey(normalized)
	if err != nil {
		return nil, normalizeServiceError(err)
	}

	if cached, found, err := s.loadCachedCallPlanRecord(source.Name, normalized, scope, selectorKey); err != nil {
		return nil, normalizeServiceError(err)
	} else if found && options.Offline {
		return cached.Payload, nil
	}
	if options.Offline {
		return nil, &NotFoundError{Message: fmt.Sprintf("offline cache miss: call plan for repo %q", normalized.Repo)}
	}

	client, err := s.newTransportClient(source)
	if err != nil {
		return nil, err
	}

	prepared, err := s.catalogService.PrepareCall(ctx, client, source.Name, normalized, catalog.RefreshOptions{
		Refresh: options.Refresh,
		Offline: options.Offline,
	})
	if err != nil {
		if !options.Refresh {
			if cached, found, cacheErr := s.loadCachedCallPlanRecord(source.Name, normalized, scope, selectorKey); cacheErr == nil && found {
				return cached.Payload, nil
			}
		}
		return nil, normalizeServiceError(err)
	}

	pinnedSelector := pinSelectorToPreparedSnapshot(normalized, prepared)
	body, err := client.PlanCall(ctx, pinnedSelector)
	if err != nil {
		if !options.Refresh {
			if cached, found, cacheErr := s.loadCachedCallPlanRecord(source.Name, normalized, prepared.Scope, selectorKey); cacheErr == nil && found && cacheRecordMatches(cached, prepared.Fingerprint) {
				return cached.Payload, nil
			}
		}
		return nil, normalizeServiceError(err)
	}

	if err := s.catalogStore.SaveCallPlan(
		source.Name,
		normalized.Repo,
		normalized.API,
		prepared.Scope,
		selectorKey,
		body,
		prepared.Fingerprint,
	); err != nil {
		return nil, normalizeServiceError(err)
	}
	return body, nil
}

func (s *RuntimeService) Health(ctx context.Context, options RequestOptions) ([]byte, error) {
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

	body, err := client.Health(ctx)
	if err != nil {
		return nil, normalizeServiceError(err)
	}
	return body, nil
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

func (s *RuntimeService) resolveSource(requestedProfile string, requestedTarget string) (profile.Source, error) {
	resolvedProfile, _, err := s.document.ResolveSource(requestedProfile, requestedTarget)
	if err != nil {
		return profile.Source{}, &InvalidInputError{Message: err.Error()}
	}
	return resolvedProfile, nil
}

func (s *RuntimeService) newTransportClient(source profile.Source) (transportClient, error) {
	client, err := s.newClient(source)
	if err != nil {
		return nil, normalizeServiceError(err)
	}
	return client, nil
}

func (s *RuntimeService) loadCachedSpecRecord(
	profileName string,
	selector request.Envelope,
	scope catalog.Scope,
	format SpecFormat,
) (catalog.Record, bool, error) {
	record, found, err := s.catalogStore.LoadSpec(profileName, selector.Repo, selector.API, scope, string(format))
	if err != nil || !found {
		return catalog.Record{}, found, err
	}
	return record, true, nil
}

func (s *RuntimeService) loadCachedOperationRecord(
	profileName string,
	selector request.Envelope,
	scope catalog.Scope,
	selectorKey string,
) (catalog.Record, bool, error) {
	record, found, err := s.catalogStore.LoadOperationResponse(profileName, selector.Repo, selector.API, scope, selectorKey)
	if err != nil || !found {
		return catalog.Record{}, found, err
	}
	return record, true, nil
}

func (s *RuntimeService) loadCachedCallPlanRecord(
	profileName string,
	selector request.Envelope,
	scope catalog.Scope,
	selectorKey string,
) (catalog.Record, bool, error) {
	record, found, err := s.catalogStore.LoadCallPlan(profileName, selector.Repo, selector.API, scope, selectorKey)
	if err != nil || !found {
		return catalog.Record{}, found, err
	}
	return record, true, nil
}

func normalizeServiceError(err error) error {
	if err == nil {
		return nil
	}

	var httpErr *httpclient.HTTPError
	if errors.As(err, &httpErr) {
		return &HTTPError{
			StatusCode: httpErr.StatusCode,
			Message:    httpErr.Message,
		}
	}

	if strings.Contains(err.Error(), "offline cache miss") {
		return &NotFoundError{Message: err.Error()}
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return &TransportError{Err: err}
	}
	return err
}

func operationSelectorKey(selector request.Envelope) (string, error) {
	switch {
	case strings.TrimSpace(selector.OperationID) != "":
		return "operation_id:" + strings.TrimSpace(selector.OperationID), nil
	case strings.TrimSpace(selector.Method) != "" && strings.TrimSpace(selector.Path) != "":
		return "method_path:" + strings.TrimSpace(selector.Method) + " " + strings.TrimSpace(selector.Path), nil
	default:
		return "", &InvalidInputError{Message: "operation selector is not complete"}
	}
}

func envelopeCacheKey(selector request.Envelope) (string, error) {
	body, err := json.Marshal(selector)
	if err != nil {
		return "", fmt.Errorf("encode request cache key: %w", err)
	}
	return string(body), nil
}

func cacheRecordMatches(record catalog.Record, fingerprint catalog.SnapshotFingerprint) bool {
	return record.Fingerprint == fingerprint
}

func pinSelectorToPreparedSnapshot(selector request.Envelope, prepared catalog.PreparedSnapshot) request.Envelope {
	if selector.RevisionID > 0 || strings.TrimSpace(selector.SHA) != "" {
		return selector
	}

	if prepared.Fingerprint.RevisionID < 1 && strings.TrimSpace(prepared.Fingerprint.SHA) == "" {
		return selector
	}

	pinned := selector
	if prepared.Fingerprint.RevisionID > 0 {
		pinned.RevisionID = prepared.Fingerprint.RevisionID
		pinned.SHA = ""
		return pinned
	}

	pinned.SHA = strings.TrimSpace(prepared.Fingerprint.SHA)
	return pinned
}
