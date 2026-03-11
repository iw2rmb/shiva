package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/iw2rmb/shiva/internal/cli/request"
	"github.com/spf13/cobra"
)

func NewRootCommand(serviceFactory func() (Service, error)) *cobra.Command {
	flags := &RootFlags{}

	rootCmd := &cobra.Command{
		Use:           "shiva <repo-ref> [<method> <path>]",
		Short:         "Inspect Shiva specs and plan Shiva calls",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ArbitraryArgs,
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		Example: strings.Join([]string{
			"  shiva allure/allure-deployment",
			"  shiva allure/allure-deployment#findAll_42",
			"  shiva allure/allure-deployment get /accessgroup/:id",
			"  shiva allure/allure-deployment@shiva#getUsers --dry-run",
		}, "\n"),
		RunE: func(cmd *cobra.Command, args []string) error {
			invocation, err := ParseShorthandInvocation(args, *flags)
			if err != nil {
				return err
			}

			service, err := loadService(serviceFactory)
			if err != nil {
				return err
			}

			body, err := executeInvocation(cmd.Context(), service, invocation, *flags)
			if err != nil {
				return err
			}
			return writeOutput(cmd.OutOrStdout(), body)
		},
	}

	addSharedFlags(rootCmd, flags)
	rootCmd.AddCommand(
		newListCommand(),
		newSyncCommand(),
		newBatchCommand(),
		newCompletionCommand(rootCmd),
		newHealthCommand(serviceFactory, flags),
	)
	return rootCmd
}

func addSharedFlags(command *cobra.Command, flags *RootFlags) {
	command.PersistentFlags().StringVarP(&flags.API, "api", "a", "", "API root path")
	command.PersistentFlags().StringVar(&flags.SHA, "sha", "", "8-character lowercase revision SHA")
	command.PersistentFlags().Int64Var(&flags.RevisionID, "rev", 0, "revision id")
	command.PersistentFlags().StringVar(&flags.Profile, "profile", "", "source profile name")
	command.PersistentFlags().StringVar(&flags.Target, "via", "", "execution target")
	command.PersistentFlags().BoolVar(&flags.Refresh, "refresh", false, "force refresh before execution")
	command.PersistentFlags().BoolVar(&flags.Offline, "offline", false, "forbid network refreshes")
	command.PersistentFlags().BoolVar(&flags.DryRun, "dry-run", false, "print the normalized plan without dispatch")
	command.PersistentFlags().StringVarP(&flags.Output, "output", "o", "", "output mode")
}

func executeInvocation(ctx context.Context, service Service, invocation ShorthandInvocation, flags RootFlags) ([]byte, error) {
	options := RequestOptions{
		Profile: flags.Profile,
		Refresh: flags.Refresh,
		Offline: flags.Offline,
	}

	switch invocation.Envelope.Kind {
	case request.KindSpec:
		format, err := resolveOutputMode(invocation.Envelope.Kind, flags.Output)
		if err != nil {
			return nil, err
		}
		return service.GetSpec(ctx, invocation.Envelope, options, format)
	case request.KindOperation:
		format, err := resolveOutputMode(invocation.Envelope.Kind, flags.Output)
		if err != nil {
			return nil, err
		}
		body, err := service.GetOperation(ctx, invocation.Envelope, options)
		if err != nil {
			return nil, err
		}
		if format == SpecFormatYAML {
			return ConvertJSONToYAML(body)
		}
		return body, nil
	case request.KindCall:
		if _, err := resolveOutputMode(invocation.Envelope.Kind, flags.Output); err != nil {
			return nil, err
		}
		return service.PlanCall(ctx, invocation.Envelope, options)
	default:
		return nil, fmt.Errorf("unsupported command kind %q", invocation.Envelope.Kind)
	}
}

func resolveOutputMode(kind request.Kind, output string) (SpecFormat, error) {
	mode := strings.TrimSpace(output)

	switch kind {
	case request.KindSpec:
		switch mode {
		case "", string(SpecFormatYAML):
			return SpecFormatYAML, nil
		case string(SpecFormatJSON):
			return SpecFormatJSON, nil
		default:
			return "", &InvalidInputError{Message: fmt.Sprintf("spec output must be one of: %s, %s", SpecFormatYAML, SpecFormatJSON)}
		}
	case request.KindOperation:
		switch mode {
		case "", string(SpecFormatJSON):
			return SpecFormatJSON, nil
		case string(SpecFormatYAML):
			return SpecFormatYAML, nil
		default:
			return "", &InvalidInputError{Message: fmt.Sprintf("operation output must be one of: %s, %s", SpecFormatJSON, SpecFormatYAML)}
		}
	case request.KindCall:
		switch mode {
		case "", string(SpecFormatJSON):
			return SpecFormatJSON, nil
		default:
			return "", &InvalidInputError{Message: "call planning output must be json"}
		}
	default:
		return "", &InvalidInputError{Message: fmt.Sprintf("unsupported command kind %q", kind)}
	}
}

func newListCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "ls",
		Short:         "List Shiva inventory",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("ls is not implemented yet")
		},
	}
}

func newSyncCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "sync <repo-ref>",
		Short:         "Refresh Shiva catalog data",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("sync is not implemented yet")
		},
	}
}

func newBatchCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "batch",
		Short:         "Execute Shiva request envelopes in batch",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("batch is not implemented yet")
		},
	}
}

func newHealthCommand(serviceFactory func() (Service, error), flags *RootFlags) *cobra.Command {
	return &cobra.Command{
		Use:           "health",
		Short:         "Check Shiva CLI source health",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := loadService(serviceFactory)
			if err != nil {
				return err
			}

			body, err := service.Health(cmd.Context(), RequestOptions{
				Profile: flags.Profile,
			})
			if err != nil {
				return err
			}
			return writeOutput(cmd.OutOrStdout(), body)
		},
	}
}

func loadService(factory func() (Service, error)) (Service, error) {
	if factory == nil {
		return nil, fmt.Errorf("service factory is not configured")
	}

	service, err := factory()
	if err != nil {
		return nil, err
	}
	if service == nil {
		return nil, fmt.Errorf("service factory returned nil service")
	}
	return service, nil
}

func writeOutput(writer io.Writer, body []byte) error {
	if writer == nil {
		return fmt.Errorf("stdout writer is not configured")
	}

	if _, err := writer.Write(body); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	if len(body) > 0 && body[len(body)-1] == '\n' {
		return nil
	}
	_, err := io.WriteString(writer, "\n")
	return err
}

func newCompletionCommand(rootCmd *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate a shell completion script",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &InvalidInputError{
					Message: "completion requires exactly one shell name",
				}
			}
			return nil
		},
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		SilenceUsage:          true,
		SilenceErrors:         true,
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return generateCompletion(cmd.Context(), rootCmd, cmd.OutOrStdout(), args[0])
		},
	}
}

func generateCompletion(ctx context.Context, rootCmd *cobra.Command, writer io.Writer, shell string) error {
	_ = ctx

	switch shell {
	case "bash":
		return rootCmd.GenBashCompletionV2(writer, true)
	case "zsh":
		return rootCmd.GenZshCompletion(writer)
	case "fish":
		return rootCmd.GenFishCompletion(writer, true)
	case "powershell":
		return rootCmd.GenPowerShellCompletionWithDesc(writer)
	default:
		return &InvalidInputError{
			Message: fmt.Sprintf("unsupported shell %q", shell),
		}
	}
}
