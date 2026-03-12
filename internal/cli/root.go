package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/iw2rmb/shiva/internal/cli/completion"
	"github.com/iw2rmb/shiva/internal/cli/request"
	"github.com/spf13/cobra"
)

func NewRootCommand(serviceFactory func() (Service, error)) *cobra.Command {
	flags := &RootFlags{}
	completionProvider := completion.NewProvider()

	rootCmd := &cobra.Command{
		Use:               "shiva <repo-ref> [<method> <path>]",
		Short:             "Inspect Shiva specs and plan Shiva calls",
		SilenceUsage:      true,
		SilenceErrors:     true,
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: completionProvider.CompleteRootArg,
		Example: strings.Join([]string{
			"  shiva allure/allure-deployment",
			"  shiva allure/allure-deployment#findAll_42",
			"  shiva allure/allure-deployment get /accessgroup/:id",
			"  shiva allure/allure-deployment@prod#getUsers --path id=42",
			"  shiva allure/allure-deployment@shiva#getUsers --dry-run",
		}, "\n"),
		RunE: func(cmd *cobra.Command, args []string) error {
			invocation, err := ParseShorthandInvocation(args, *flags)
			if err != nil {
				return err
			}
			invocation.Envelope, err = request.ApplyCLIInputs(invocation.Envelope, requestInputsFromFlags(*flags))
			if err != nil {
				return normalizeCLIValidation(err)
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
	listCmd := newListCommand(serviceFactory, flags, completionProvider)
	syncCmd := newSyncCommand(serviceFactory, flags)
	syncCmd.ValidArgsFunction = completionProvider.CompleteRepoArg
	rootCmd.AddCommand(
		listCmd,
		syncCmd,
		newBatchCommand(serviceFactory, flags),
		newCompletionCommand(rootCmd),
		newHealthCommand(serviceFactory, flags),
	)
	mustRegisterCompletionFunc(rootCmd, "api", completionProvider.CompleteAPIFlag)
	mustRegisterCompletionFunc(rootCmd, "profile", completionProvider.CompleteProfileFlag)
	mustRegisterCompletionFunc(rootCmd, "via", completionProvider.CompleteTargetFlag)
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
	outputValue := newStringFlagValue(&flags.Output, "yaml|json|body|curl|table|tsv|ndjson")
	command.PersistentFlags().VarP(outputValue, "output", "o", "output mode")
	command.PersistentFlags().StringArrayVar(&flags.Path, "path", nil, "request path parameter key=value")
	command.PersistentFlags().StringArrayVar(&flags.Query, "query", nil, "request query parameter key=value")
	command.PersistentFlags().StringArrayVar(&flags.Header, "header", nil, "request header Name=value")
	command.PersistentFlags().StringVar(&flags.JSON, "json", "", "inline json or @file request body")
	command.PersistentFlags().StringVar(&flags.Body, "body", "", "@file request body")
}

func mustRegisterCompletionFunc(
	command *cobra.Command,
	flagName string,
	fn func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective),
) {
	if err := command.RegisterFlagCompletionFunc(flagName, fn); err != nil {
		panic(err)
	}
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
		format, err := resolveCallOutputMode(invocation.Envelope.DryRun, flags.Output)
		if err != nil {
			return nil, err
		}
		return service.ExecuteCall(ctx, invocation.Envelope, options, format)
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

func resolveCallOutputMode(dryRun bool, output string) (CallFormat, error) {
	mode := strings.TrimSpace(output)

	if dryRun {
		switch mode {
		case "", string(CallFormatJSON):
			return CallFormatJSON, nil
		case string(CallFormatCurl):
			return CallFormatCurl, nil
		default:
			return "", &InvalidInputError{Message: "dry-run output must be one of: json, curl"}
		}
	}

	switch mode {
	case "", string(CallFormatBody):
		return CallFormatBody, nil
	case string(CallFormatJSON):
		return CallFormatJSON, nil
	default:
		return "", &InvalidInputError{Message: "call output must be one of: body, json"}
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
			if err := validateHealthFlags(*flags); err != nil {
				return err
			}

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

func validateHealthFlags(flags RootFlags) error {
	switch {
	case flags.API != "":
		return &InvalidInputError{Message: "health does not accept --api"}
	case flags.SHA != "" || flags.RevisionID > 0:
		return &InvalidInputError{Message: "health does not accept --sha or --rev"}
	case flags.Target != "":
		return &InvalidInputError{Message: "health does not accept --via"}
	case flags.Refresh:
		return &InvalidInputError{Message: "health does not accept --refresh"}
	case flags.Offline:
		return &InvalidInputError{Message: "health does not accept --offline"}
	case flags.DryRun:
		return &InvalidInputError{Message: "health does not accept --dry-run"}
	case flags.Output != "":
		return &InvalidInputError{Message: "health does not accept --output"}
	default:
		return validateNoCallInputFlags(flags, "health")
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
	if len(body) == 0 {
		return nil
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
		return generatePatchedCompletion(writer, func(writer io.Writer) error {
			return rootCmd.GenBashCompletionV2(writer, true)
		}, patchBashCompletionScript)
	case "zsh":
		return generatePatchedCompletion(writer, rootCmd.GenZshCompletion, patchZshCompletionScript)
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

func generatePatchedCompletion(
	writer io.Writer,
	generate func(writer io.Writer) error,
	patch func(string) string,
) error {
	buffer := &bytes.Buffer{}
	if err := generate(buffer); err != nil {
		return err
	}
	_, err := io.WriteString(writer, patch(buffer.String()))
	return err
}

func patchZshCompletionScript(script string) string {
	const injectionPoint = `out=$(eval ${requestComp} 2>/dev/null)
    __shiva_debug "completion output: ${out}"
`
	const injected = `out=$(eval ${requestComp} 2>/dev/null)
    __shiva_debug "completion output: ${out}"
    if [ ${#words[@]} -le 2 ]; then
        out=$(__shiva_filter_root_command_completions "${out}")
    fi
`
	script = strings.Replace(script, injectionPoint, injected, 1)
	return strings.Replace(script, `__shiva_debug()
{
    local file="$BASH_COMP_DEBUG_FILE"
    if [[ -n ${file} ]]; then
        echo "$*" >> "${file}"
    fi
}
`, `__shiva_debug()
{
    local file="$BASH_COMP_DEBUG_FILE"
    if [[ -n ${file} ]]; then
        echo "$*" >> "${file}"
    fi
}

__shiva_filter_root_command_completions()
{
    local out="$1"
    local line value
    local tab="$(printf '\t')"
    local hasRepo=0
    local filtered=""

    while IFS='\n' read -r line; do
        [[ -z "${line}" ]] && continue
        [[ "${line}" == :* ]] && continue
        [[ "${line}" == "_activeHelp_ "* ]] && continue
        value=${line%%$tab*}
        if [[ "${value}" == */* ]]; then
            hasRepo=1
            break
        fi
    done < <(printf "%s\n" "${out}")

    if [[ ${hasRepo} -eq 0 ]]; then
        printf "%s" "${out}"
        return
    fi

    while IFS='\n' read -r line; do
        [[ -z "${line}" ]] && continue
        if [[ "${line}" == :* ]] || [[ "${line}" == "_activeHelp_ "* ]]; then
            filtered+="${line}"$'\n'
            continue
        fi

        value=${line%%$tab*}
        case "${value}" in
            batch|completion|health|help|ls|sync)
                continue
                ;;
        esac
        filtered+="${line}"$'\n'
    done < <(printf "%s\n" "${out}")

    printf "%s" "${filtered%$'\n'}"
}
`, 1)
}

func patchBashCompletionScript(script string) string {
	const injectionPoint = `    out=$(eval "${requestComp}" 2>/dev/null)

    # Extract the directive integer at the very end of the output following a colon (:)
`
	const injected = `    out=$(eval "${requestComp}" 2>/dev/null)
    if [[ ${cword} -le 1 ]]; then
        out=$(__shiva_filter_root_command_completions "${out}")
    fi

    # Extract the directive integer at the very end of the output following a colon (:)
`
	script = strings.Replace(script, injectionPoint, injected, 1)
	return strings.Replace(script, `__shiva_debug()
{
    if [[ -n ${BASH_COMP_DEBUG_FILE-} ]]; then
        echo "$*" >> "${BASH_COMP_DEBUG_FILE}"
    fi
}
`, `__shiva_debug()
{
    if [[ -n ${BASH_COMP_DEBUG_FILE-} ]]; then
        echo "$*" >> "${BASH_COMP_DEBUG_FILE}"
    fi
}

__shiva_filter_root_command_completions()
{
    local out="$1"
    local line value
    local tab=$'\t'
    local hasRepo=0
    local filtered=""

    while IFS='' read -r line; do
        [[ -z "${line}" ]] && continue
        [[ "${line}" == :* ]] && continue
        [[ "${line}" == "_activeHelp_ "* ]] && continue
        value=${line%%$tab*}
        if [[ "${value}" == */* ]]; then
            hasRepo=1
            break
        fi
    done <<<"${out}"

    if [[ ${hasRepo} -eq 0 ]]; then
        printf "%s" "${out}"
        return
    fi

    while IFS='' read -r line; do
        [[ -z "${line}" ]] && continue
        if [[ "${line}" == :* ]] || [[ "${line}" == "_activeHelp_ "* ]]; then
            filtered+="${line}"$'\n'
            continue
        fi

        value=${line%%$tab*}
        case "${value}" in
            batch|completion|health|help|ls|sync)
                continue
                ;;
        esac
        filtered+="${line}"$'\n'
    done <<<"${out}"

    printf "%s" "${filtered%$'\n'}"
}
`, 1)
}
