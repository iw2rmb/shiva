package cli

import (
	"context"
	"fmt"
	"strings"

	clioutput "github.com/iw2rmb/shiva/internal/cli/output"
	"github.com/iw2rmb/shiva/internal/cli/request"
	"github.com/iw2rmb/shiva/internal/repoid"
	"github.com/iw2rmb/shiva/internal/tui"
	"github.com/spf13/cobra"
)

var runTUI = tui.Run

func newTUICommand(serviceFactory func() (Service, error), flags *RootFlags) *cobra.Command {
	return &cobra.Command{
		Use:           "tui [selector]",
		Short:         "Browse Shiva specs in a terminal UI",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateTUIFlags(*flags); err != nil {
				return err
			}

			route, err := parseTUIInitialRoute(args)
			if err != nil {
				return err
			}

			service, err := loadService(serviceFactory)
			if err != nil {
				return err
			}

			return runTUI(
				cmd.Context(),
				cmd.InOrStdin(),
				cmd.OutOrStdout(),
				tuiServiceAdapter{service: service},
				route,
				tui.RequestOptions{
					Profile: flags.Profile,
					Offline: flags.Offline,
				},
			)
		},
	}
}

func parseTUIInitialRoute(args []string) (tui.InitialRoute, error) {
	if len(args) == 0 {
		return tui.InitialRoute{Kind: tui.RouteNamespaces}, nil
	}

	selector := strings.TrimSpace(args[0])
	if selector == "" {
		return tui.InitialRoute{}, &InvalidInputError{Message: "tui selector must not be empty"}
	}
	if strings.Contains(selector, "#") || strings.Contains(selector, "@") {
		return tui.InitialRoute{}, &InvalidInputError{Message: "tui selector must be <namespace>/ or <namespace>/<repo>"}
	}
	if strings.HasSuffix(selector, "/") {
		namespace := strings.TrimSpace(strings.TrimSuffix(selector, "/"))
		if namespace == "" || strings.HasSuffix(namespace, "/") {
			return tui.InitialRoute{}, &InvalidInputError{Message: "tui namespace must not be empty"}
		}
		return tui.InitialRoute{
			Kind:      tui.RouteRepos,
			Namespace: namespace,
		}, nil
	}

	identity, err := repoid.ParsePath(selector)
	if err != nil {
		return tui.InitialRoute{}, &InvalidInputError{Message: "tui selector must be <namespace>/ or <namespace>/<repo>"}
	}
	return tui.InitialRoute{
		Kind:      tui.RouteRepoExplorer,
		Namespace: identity.Namespace,
		Repo:      identity.Repo,
	}, nil
}

func validateTUIFlags(flags RootFlags) error {
	switch {
	case flags.API != "":
		return &InvalidInputError{Message: "tui does not accept --api"}
	case flags.SHA != "" || flags.RevisionID > 0:
		return &InvalidInputError{Message: "tui does not accept --sha or --rev"}
	case flags.Target != "":
		return &InvalidInputError{Message: "tui does not accept --via"}
	case flags.DryRun:
		return &InvalidInputError{Message: "tui does not accept --dry-run"}
	case flags.Output != "":
		return &InvalidInputError{Message: "tui does not accept --output"}
	default:
		return validateNoCallInputFlags(flags, "tui")
	}
}

type tuiServiceAdapter struct {
	service Service
}

func (adapter tuiServiceAdapter) ListRepos(
	ctx context.Context,
	options tui.RequestOptions,
	format clioutput.ListFormat,
) ([]byte, error) {
	return adapter.service.ListRepos(ctx, fromTUIRequestOptions(options), format)
}

func (adapter tuiServiceAdapter) ListOperations(
	ctx context.Context,
	selector request.Envelope,
	options tui.RequestOptions,
	format clioutput.ListFormat,
) ([]byte, error) {
	return adapter.service.ListOperations(ctx, selector, fromTUIRequestOptions(options), format)
}

func (adapter tuiServiceAdapter) GetOperation(
	ctx context.Context,
	selector request.Envelope,
	options tui.RequestOptions,
) ([]byte, error) {
	return adapter.service.GetOperation(ctx, selector, fromTUIRequestOptions(options))
}

func (adapter tuiServiceAdapter) GetSpec(
	ctx context.Context,
	selector request.Envelope,
	options tui.RequestOptions,
	format tui.SpecFormat,
) ([]byte, error) {
	switch format {
	case tui.SpecFormatJSON:
		return adapter.service.GetSpec(ctx, selector, fromTUIRequestOptions(options), SpecFormatJSON)
	case tui.SpecFormatYAML:
		return adapter.service.GetSpec(ctx, selector, fromTUIRequestOptions(options), SpecFormatYAML)
	default:
		return nil, fmt.Errorf("unsupported tui spec format %q", format)
	}
}

func fromTUIRequestOptions(options tui.RequestOptions) RequestOptions {
	return RequestOptions{
		Profile: options.Profile,
		Offline: options.Offline,
	}
}
