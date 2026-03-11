package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func NewRootCommand(serviceFactory func() (Service, error)) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "shiva <repo-path>|<repo-path>#<operationId>",
		Short:         "Fetch Shiva specs and operations",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &InvalidInputError{
					Message: "expected exactly one selector argument",
				}
			}
			return nil
		},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		Example: "  shiva allure/allure-deployment\n  shiva allure/allure-deployment#findAll_42",
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := loadService(serviceFactory)
			if err != nil {
				return err
			}

			selector, err := ParseSelector(args[0])
			if err != nil {
				return err
			}

			var body []byte
			if selector.HasOperation() {
				body, err = service.GetOperation(cmd.Context(), selector.RepoPath, selector.OperationID)
			} else {
				body, err = service.GetRepoSpec(cmd.Context(), selector.RepoPath)
			}
			if err != nil {
				return err
			}

			return writeOutput(cmd.OutOrStdout(), body)
		},
	}

	rootCmd.AddCommand(newCompletionCommand(rootCmd))
	return rootCmd
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
