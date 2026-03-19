package tui

import (
	"context"

	clioutput "github.com/iw2rmb/shiva/internal/cli/output"
	"github.com/iw2rmb/shiva/internal/cli/request"
)

type RequestOptions struct {
	Profile string
	Offline bool
}

type SpecFormat string

const (
	SpecFormatJSON SpecFormat = "json"
	SpecFormatYAML SpecFormat = "yaml"
)

type BrowserService interface {
	ListNamespaces(ctx context.Context, options RequestOptions, format clioutput.ListFormat) ([]byte, error)
	ListRepos(ctx context.Context, options RequestOptions, format clioutput.ListFormat) ([]byte, error)
	ListOperations(ctx context.Context, selector request.Envelope, options RequestOptions, format clioutput.ListFormat) ([]byte, error)
	GetOperation(ctx context.Context, selector request.Envelope, options RequestOptions) ([]byte, error)
	GetSpec(ctx context.Context, selector request.Envelope, options RequestOptions, format SpecFormat) ([]byte, error)
}
