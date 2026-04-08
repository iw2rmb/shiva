package tui

import (
	"context"

	clioutput "github.com/iw2rmb/shiva/internal/cli/output"
	"github.com/iw2rmb/shiva/internal/cli/request"
)

type RequestOptions struct {
	Profile   string
	Offline   bool
	Limit     int32
	Offset    int32
	Query     string
	Namespace string
}

type SpecFormat string

const (
	SpecFormatJSON SpecFormat = "json"
	SpecFormatYAML SpecFormat = "yaml"
)

type BrowserService interface {
	CountNamespaces(ctx context.Context, options RequestOptions) (CatalogCount, error)
	CountRepos(ctx context.Context, namespace string, options RequestOptions) (CatalogCount, error)
	CountAPIs(ctx context.Context, selector request.Envelope, options RequestOptions) (CatalogCount, error)
	CountOperations(ctx context.Context, selector request.Envelope, options RequestOptions) (CatalogCount, error)
	ListNamespaces(ctx context.Context, options RequestOptions, format clioutput.ListFormat) ([]byte, error)
	ListRepos(ctx context.Context, options RequestOptions, format clioutput.ListFormat) ([]byte, error)
	ListAPIs(ctx context.Context, selector request.Envelope, options RequestOptions, format clioutput.ListFormat) ([]byte, error)
	ListOperations(ctx context.Context, selector request.Envelope, options RequestOptions, format clioutput.ListFormat) ([]byte, error)
	GetOperation(ctx context.Context, selector request.Envelope, options RequestOptions) ([]byte, error)
	GetSpec(ctx context.Context, selector request.Envelope, options RequestOptions, format SpecFormat) ([]byte, error)
}

type CatalogCount struct {
	TotalCount    int64
	MaxItemLength int64
}
