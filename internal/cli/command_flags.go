package cli

import "github.com/iw2rmb/shiva/internal/cli/request"

func requestInputsFromFlags(flags RootFlags) request.CLIInputs {
	return request.CLIInputs{
		Path:   append([]string(nil), flags.Path...),
		Query:  append([]string(nil), flags.Query...),
		Header: append([]string(nil), flags.Header...),
		JSON:   flags.JSON,
		Body:   flags.Body,
	}
}

func hasCallInputFlags(flags RootFlags) bool {
	inputs := requestInputsFromFlags(flags)
	return len(inputs.Path) > 0 ||
		len(inputs.Query) > 0 ||
		len(inputs.Header) > 0 ||
		inputs.JSON != "" ||
		inputs.Body != ""
}

func validateNoCallInputFlags(flags RootFlags, commandName string) error {
	if !hasCallInputFlags(flags) {
		return nil
	}
	return &InvalidInputError{Message: commandName + " does not accept --path, --query, --header, --json, or --body"}
}
