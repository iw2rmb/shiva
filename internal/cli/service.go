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
	clioutput "github.com/iw2rmb/shiva/internal/cli/output"
	"github.com/iw2rmb/shiva/internal/cli/profile"
	"github.com/iw2rmb/shiva/internal/cli/request"
	"gopkg.in/yaml.v3"
)

type RequestOptions struct {
	Profile   string
	Offline   bool
	Limit     int32
	Offset    int32
	Query     string
	Namespace string
}

type Service interface {
	GetSpec(ctx context.Context, selector request.Envelope, options RequestOptions, format SpecFormat) ([]byte, error)
	GetOperation(ctx context.Context, selector request.Envelope, options RequestOptions) ([]byte, error)
	GetAPIIssues(ctx context.Context, selector request.Envelope, options RequestOptions) ([]byte, error)
	ExecuteCall(ctx context.Context, selector request.Envelope, options RequestOptions, format CallFormat) ([]byte, error)
	CountNamespaces(ctx context.Context, options RequestOptions) (int64, error)
	CountNamespaceCatalog(ctx context.Context, options RequestOptions) (CatalogCount, error)
	CountRepoCatalog(ctx context.Context, namespace string, options RequestOptions) (CatalogCount, error)
	CountAPICatalog(ctx context.Context, selector request.Envelope, options RequestOptions) (CatalogCount, error)
	CountOperationCatalog(ctx context.Context, selector request.Envelope, options RequestOptions) (CatalogCount, error)
	ListNamespaces(ctx context.Context, options RequestOptions, format clioutput.ListFormat) ([]byte, error)
	ListRepos(ctx context.Context, options RequestOptions, format clioutput.ListFormat) ([]byte, error)
	ListAPIs(ctx context.Context, selector request.Envelope, options RequestOptions, format clioutput.ListFormat) ([]byte, error)
	ListOperations(ctx context.Context, selector request.Envelope, options RequestOptions, format clioutput.ListFormat) ([]byte, error)
	EmitRepoRequests(ctx context.Context, options RequestOptions) ([]byte, error)
	EmitAPIRequests(ctx context.Context, selector request.Envelope, options RequestOptions) ([]byte, error)
	EmitOperationRequests(ctx context.Context, selector request.Envelope, options RequestOptions, targetName string) ([]byte, error)
	Sync(ctx context.Context, selector request.Envelope, options RequestOptions) ([]byte, error)
	Health(ctx context.Context, options RequestOptions) ([]byte, error)
}

type transportClient interface {
	GetSpec(ctx context.Context, selector request.Envelope, format SpecFormat) ([]byte, error)
	GetOperation(ctx context.Context, selector request.Envelope) ([]byte, error)
	GetAPIIssues(ctx context.Context, selector request.Envelope) ([]byte, error)
	CountNamespaces(ctx context.Context) ([]byte, error)
	CountRepos(ctx context.Context, namespace string) ([]byte, error)
	CountAPIs(ctx context.Context, selector request.Envelope) ([]byte, error)
	CountOperations(ctx context.Context, selector request.Envelope) ([]byte, error)
	ListNamespaces(ctx context.Context) ([]byte, error)
	ListRepos(ctx context.Context) ([]byte, error)
	GetCatalogStatus(ctx context.Context, repo string) ([]byte, error)
	ListAPIs(ctx context.Context, selector request.Envelope) ([]byte, error)
	ListOperations(ctx context.Context, selector request.Envelope) ([]byte, error)
	Health(ctx context.Context) ([]byte, error)
}

type pagedTransportClient interface {
	ListNamespacesPage(ctx context.Context, limit int32, offset int32) ([]byte, error)
	ListReposPage(ctx context.Context, namespace string, limit int32, offset int32) ([]byte, error)
	ListAPIsPage(ctx context.Context, selector request.Envelope, limit int32, offset int32) ([]byte, error)
	ListOperationsPage(ctx context.Context, selector request.Envelope, limit int32, offset int32) ([]byte, error)
}

type filteredPagedTransportClient interface {
	ListNamespacesPageFiltered(ctx context.Context, query string, limit int32, offset int32) ([]byte, error)
	ListReposPageFiltered(ctx context.Context, namespace string, query string, limit int32, offset int32) ([]byte, error)
	ListAPIsPageFiltered(ctx context.Context, selector request.Envelope, query string, limit int32, offset int32) ([]byte, error)
	ListOperationsPageFiltered(ctx context.Context, selector request.Envelope, query string, limit int32, offset int32) ([]byte, error)
}

type filteredCountTransportClient interface {
	CountNamespacesFiltered(ctx context.Context, query string) ([]byte, error)
	CountReposFiltered(ctx context.Context, namespace string, query string) ([]byte, error)
	CountAPIsFiltered(ctx context.Context, selector request.Envelope, query string) ([]byte, error)
	CountOperationsFiltered(ctx context.Context, selector request.Envelope, query string) ([]byte, error)
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
	if s == nil || s.newClient == nil {
		return nil, fmt.Errorf("CLI service is not configured")
	}

	normalized, err := request.NormalizeEnvelope(selector, request.NormalizeOptions{
		DefaultKind:      request.KindSpec,
		AllowMissingKind: true,
	})
	if err != nil {
		return nil, normalizeCLIValidation(err)
	}

	source, err := s.resolveSource(options.Profile, normalized.Target)
	if err != nil {
		return nil, err
	}
	if options.Offline {
		return nil, &InvalidInputError{Message: "offline mode is not supported"}
	}

	client, err := s.newTransportClient(source)
	if err != nil {
		return nil, err
	}

	body, err := client.GetSpec(ctx, normalized, format)
	if err != nil {
		return nil, normalizeServiceError(err)
	}
	return body, nil
}

func (s *RuntimeService) GetOperation(
	ctx context.Context,
	selector request.Envelope,
	options RequestOptions,
) ([]byte, error) {
	if s == nil || s.newClient == nil {
		return nil, fmt.Errorf("CLI service is not configured")
	}

	normalized, err := request.NormalizeEnvelope(selector, request.NormalizeOptions{
		DefaultKind:      request.KindOperation,
		AllowMissingKind: true,
	})
	if err != nil {
		return nil, normalizeCLIValidation(err)
	}

	source, err := s.resolveSource(options.Profile, normalized.Target)
	if err != nil {
		return nil, err
	}
	if options.Offline {
		return nil, &InvalidInputError{Message: "offline mode is not supported"}
	}

	client, err := s.newTransportClient(source)
	if err != nil {
		return nil, err
	}

	body, err := client.GetOperation(ctx, normalized)
	if err != nil {
		return nil, normalizeServiceError(err)
	}
	return body, nil
}

func (s *RuntimeService) GetAPIIssues(
	ctx context.Context,
	selector request.Envelope,
	options RequestOptions,
) ([]byte, error) {
	if s == nil || s.newClient == nil {
		return nil, fmt.Errorf("CLI service is not configured")
	}

	normalized, err := request.NormalizeEnvelope(selector, request.NormalizeOptions{
		DefaultKind:      request.KindSpec,
		AllowMissingKind: true,
	})
	if err != nil {
		return nil, normalizeCLIValidation(err)
	}
	if strings.TrimSpace(normalized.API) == "" {
		return nil, &InvalidInputError{Message: "api must not be empty"}
	}

	source, err := s.resolveSource(options.Profile, normalized.Target)
	if err != nil {
		return nil, err
	}
	if options.Offline {
		return nil, &InvalidInputError{Message: "offline mode is not supported"}
	}

	client, err := s.newTransportClient(source)
	if err != nil {
		return nil, err
	}

	body, err := client.GetAPIIssues(ctx, normalized)
	if err != nil {
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
	resolvedProfile, _, err := s.resolveSourceAndTarget(requestedProfile, requestedTarget)
	if err != nil {
		return profile.Source{}, err
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

func normalizeServiceError(err error) error {
	if err == nil {
		return nil
	}

	var httpErr *httpclient.HTTPError
	if errors.As(err, &httpErr) {
		if conflictErr := normalizeHTTPConflict(httpErr); conflictErr != nil {
			return conflictErr
		}
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
