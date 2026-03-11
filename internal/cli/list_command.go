package cli

import (
	"context"
	"io"
	"os"
	"strings"

	"github.com/iw2rmb/shiva/internal/cli/completion"
	clioutput "github.com/iw2rmb/shiva/internal/cli/output"
	"github.com/iw2rmb/shiva/internal/cli/request"
	"github.com/spf13/cobra"
)

type ListFlags struct {
	Emit string
}

func newListCommand(serviceFactory func() (Service, error), flags *RootFlags, completionProvider *completion.Provider) *cobra.Command {
	listFlags := &ListFlags{}
	command := &cobra.Command{
		Use:           "ls",
		Short:         "List Shiva inventory",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return &InvalidInputError{Message: "ls requires one of: repos, apis, ops"}
		},
	}
	command.PersistentFlags().StringVar(&listFlags.Emit, "emit", "", "emit request envelopes")

	reposCmd := newListReposCommand(serviceFactory, flags, listFlags)
	apisCmd := newListAPIsCommand(serviceFactory, flags, listFlags)
	opsCmd := newListOperationsCommand(serviceFactory, flags, listFlags)
	apisCmd.ValidArgsFunction = completionProvider.CompleteRepoArg
	opsCmd.ValidArgsFunction = completionProvider.CompleteRepoArg

	command.AddCommand(
		reposCmd,
		apisCmd,
		opsCmd,
	)
	return command
}

func newListReposCommand(serviceFactory func() (Service, error), flags *RootFlags, listFlags *ListFlags) *cobra.Command {
	return &cobra.Command{
		Use:           "repos",
		Short:         "List cached repos",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateRefreshOfflineFlags(*flags); err != nil {
				return err
			}
			if err := validateListReposFlags(*flags, listFlags.Emit); err != nil {
				return err
			}

			service, err := loadService(serviceFactory)
			if err != nil {
				return err
			}
			if listFlags.Emit == "request" {
				body, err := service.EmitRepoRequests(cmd.Context(), requestOptionsFromFlags(*flags))
				if err != nil {
					return err
				}
				return writeOutput(cmd.OutOrStdout(), body)
			}

			format, err := resolveListOutputMode(flags.Output, cmd.OutOrStdout())
			if err != nil {
				return err
			}

			body, err := service.ListRepos(cmd.Context(), requestOptionsFromFlags(*flags), format)
			if err != nil {
				return err
			}
			return writeOutput(cmd.OutOrStdout(), body)
		},
	}
}

func newListAPIsCommand(serviceFactory func() (Service, error), flags *RootFlags, listFlags *ListFlags) *cobra.Command {
	return &cobra.Command{
		Use:           "apis <repo-ref>",
		Short:         "List APIs for one repo snapshot",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateRefreshOfflineFlags(*flags); err != nil {
				return err
			}
			selector, err := parseRepoSnapshotSelector(args[0], *flags, false, false)
			if err != nil {
				return err
			}
			if err := validateListAPIsFlags(*flags, listFlags.Emit); err != nil {
				return err
			}

			service, err := loadService(serviceFactory)
			if err != nil {
				return err
			}
			if listFlags.Emit == "request" {
				body, err := service.EmitAPIRequests(cmd.Context(), selector, requestOptionsFromFlags(*flags))
				if err != nil {
					return err
				}
				return writeOutput(cmd.OutOrStdout(), body)
			}

			format, err := resolveListOutputMode(flags.Output, cmd.OutOrStdout())
			if err != nil {
				return err
			}

			body, err := service.ListAPIs(cmd.Context(), selector, requestOptionsFromFlags(*flags), format)
			if err != nil {
				return err
			}
			return writeOutput(cmd.OutOrStdout(), body)
		},
	}
}

func newListOperationsCommand(serviceFactory func() (Service, error), flags *RootFlags, listFlags *ListFlags) *cobra.Command {
	return &cobra.Command{
		Use:           "ops <repo-ref>",
		Short:         "List operations for one repo snapshot",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateRefreshOfflineFlags(*flags); err != nil {
				return err
			}
			selector, err := parseRepoSnapshotSelector(args[0], *flags, true, listFlags.Emit == "request")
			if err != nil {
				return err
			}
			if err := validateListOperationsFlags(*flags, listFlags.Emit); err != nil {
				return err
			}

			service, err := loadService(serviceFactory)
			if err != nil {
				return err
			}
			if listFlags.Emit == "request" {
				body, err := service.EmitOperationRequests(cmd.Context(), selector, requestOptionsFromFlags(*flags), flags.Target)
				if err != nil {
					return err
				}
				return writeOutput(cmd.OutOrStdout(), body)
			}

			format, err := resolveListOutputMode(flags.Output, cmd.OutOrStdout())
			if err != nil {
				return err
			}

			body, err := service.ListOperations(cmd.Context(), selector, requestOptionsFromFlags(*flags), format)
			if err != nil {
				return err
			}
			return writeOutput(cmd.OutOrStdout(), body)
		},
	}
}

func requestOptionsFromFlags(flags RootFlags) RequestOptions {
	return RequestOptions{
		Profile: flags.Profile,
		Refresh: flags.Refresh,
		Offline: flags.Offline,
	}
}

func validateRefreshOfflineFlags(flags RootFlags) error {
	if flags.Refresh && flags.Offline {
		return &InvalidInputError{Message: "--refresh and --offline are mutually exclusive"}
	}
	return nil
}

func parseRepoSnapshotSelector(raw string, flags RootFlags, allowAPI bool, allowTarget bool) (request.Envelope, error) {
	packed, err := ParsePackedSelector(raw)
	if err != nil {
		return request.Envelope{}, err
	}
	if packed.HasTarget() {
		return request.Envelope{}, &InvalidInputError{Message: "this command does not accept @target selectors"}
	}
	if packed.HasOperation() {
		return request.Envelope{}, &InvalidInputError{Message: "this command does not accept #<operation-id> selectors"}
	}
	if flags.Target != "" && !allowTarget {
		return request.Envelope{}, &InvalidInputError{Message: "this command does not accept --via"}
	}
	if flags.DryRun {
		return request.Envelope{}, &InvalidInputError{Message: "this command does not accept --dry-run"}
	}
	if err := validateNoCallInputFlags(flags, "this command"); err != nil {
		return request.Envelope{}, err
	}
	if !allowAPI && flags.API != "" {
		return request.Envelope{}, &InvalidInputError{Message: "this command does not accept --api"}
	}

	return request.Envelope{
		Repo:       packed.RepoPath,
		API:        flags.API,
		RevisionID: flags.RevisionID,
		SHA:        flags.SHA,
	}, nil
}

func resolveListOutputMode(value string, writer io.Writer) (clioutput.ListFormat, error) {
	switch value {
	case "":
		if isTTYWriter(writer) {
			return clioutput.ListFormatTable, nil
		}
		return clioutput.ListFormatNDJSON, nil
	case string(clioutput.ListFormatTable):
		return clioutput.ListFormatTable, nil
	case string(clioutput.ListFormatTSV):
		return clioutput.ListFormatTSV, nil
	case string(clioutput.ListFormatJSON):
		return clioutput.ListFormatJSON, nil
	case string(clioutput.ListFormatNDJSON):
		return clioutput.ListFormatNDJSON, nil
	default:
		return "", &InvalidInputError{Message: "list output must be one of: table, tsv, json, ndjson"}
	}
}

func isTTYWriter(writer io.Writer) bool {
	file, ok := writer.(*os.File)
	if !ok || file == nil {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func validateListReposFlags(flags RootFlags, emit string) error {
	if err := validateListEmit(emit); err != nil {
		return err
	}
	switch {
	case flags.API != "":
		return &InvalidInputError{Message: "ls repos does not accept --api"}
	case flags.SHA != "" || flags.RevisionID > 0:
		return &InvalidInputError{Message: "ls repos does not accept --sha or --rev"}
	case flags.Target != "":
		return &InvalidInputError{Message: "ls repos does not accept --via"}
	case flags.DryRun:
		return &InvalidInputError{Message: "ls repos does not accept --dry-run"}
	case emit == "request" && flags.Output != "":
		return &InvalidInputError{Message: "ls repos --emit request does not accept --output"}
	default:
		return validateNoCallInputFlags(flags, "ls repos")
	}
}

func validateListAPIsFlags(flags RootFlags, emit string) error {
	if err := validateListEmit(emit); err != nil {
		return err
	}
	switch {
	case flags.API != "":
		return &InvalidInputError{Message: "ls apis does not accept --api"}
	case flags.Target != "":
		return &InvalidInputError{Message: "ls apis does not accept --via"}
	case flags.DryRun:
		return &InvalidInputError{Message: "ls apis does not accept --dry-run"}
	case emit == "request" && flags.Output != "":
		return &InvalidInputError{Message: "ls apis --emit request does not accept --output"}
	default:
		return validateNoCallInputFlags(flags, "ls apis")
	}
}

func validateListOperationsFlags(flags RootFlags, emit string) error {
	if err := validateListEmit(emit); err != nil {
		return err
	}
	switch {
	case flags.Target != "" && emit != "request":
		return &InvalidInputError{Message: "ls ops does not accept --via"}
	case flags.DryRun:
		return &InvalidInputError{Message: "ls ops does not accept --dry-run"}
	case emit == "request" && flags.Output != "":
		return &InvalidInputError{Message: "ls ops --emit request does not accept --output"}
	default:
		return validateNoCallInputFlags(flags, "ls ops")
	}
}

func executeSyncCommand(ctx context.Context, service Service, selector request.Envelope, flags RootFlags) ([]byte, error) {
	if flags.Output != "" && flags.Output != "json" {
		return nil, &InvalidInputError{Message: "sync output must be json"}
	}
	return service.Sync(ctx, selector, requestOptionsFromFlags(flags))
}

func newSyncCommand(serviceFactory func() (Service, error), flags *RootFlags) *cobra.Command {
	return &cobra.Command{
		Use:           "sync <repo-ref>",
		Short:         "Refresh Shiva catalog data",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateRefreshOfflineFlags(*flags); err != nil {
				return err
			}
			selector, err := parseRepoSnapshotSelector(args[0], *flags, false, false)
			if err != nil {
				return err
			}
			if err := validateNoCallInputFlags(*flags, "sync"); err != nil {
				return err
			}

			service, err := loadService(serviceFactory)
			if err != nil {
				return err
			}

			body, err := executeSyncCommand(cmd.Context(), service, selector, *flags)
			if err != nil {
				return err
			}
			return writeOutput(cmd.OutOrStdout(), body)
		},
	}
}

func validateListEmit(value string) error {
	switch strings.TrimSpace(value) {
	case "", "request":
		return nil
	default:
		return &InvalidInputError{Message: "ls emit mode must be: request"}
	}
}
