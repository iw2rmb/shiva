package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	clioutput "github.com/iw2rmb/shiva/internal/cli/output"
	"github.com/iw2rmb/shiva/internal/cli/request"
	"github.com/spf13/cobra"
)

type BatchFlags struct {
	From string
}

type BatchExecutionError struct {
	Err   error
	Count int
}

func (e *BatchExecutionError) Error() string {
	if e == nil || e.Err == nil {
		return "batch execution failed"
	}
	return fmt.Sprintf("batch completed with %d failed item(s): %v", e.Count, e.Err)
}

func (e *BatchExecutionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func newBatchCommand(serviceFactory func() (Service, error), flags *RootFlags) *cobra.Command {
	batchFlags := &BatchFlags{}

	command := &cobra.Command{
		Use:           "batch",
		Short:         "Execute Shiva request envelopes in batch",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateBatchFlags(*flags); err != nil {
				return err
			}

			format, err := resolveBatchOutputMode(flags.Output)
			if err != nil {
				return err
			}

			reader, closer, err := openBatchInput(cmd.InOrStdin(), batchFlags.From)
			if err != nil {
				return err
			}
			defer closer.Close()

			service, err := loadService(serviceFactory)
			if err != nil {
				return err
			}

			return executeBatchCommand(cmd.Context(), cmd.OutOrStdout(), reader, service, *flags, format)
		},
	}
	command.Flags().StringVar(&batchFlags.From, "from", "", "read NDJSON request envelopes from file")
	return command
}

func validateBatchFlags(flags RootFlags) error {
	switch {
	case flags.API != "":
		return &InvalidInputError{Message: "batch does not accept --api"}
	case flags.SHA != "" || flags.RevisionID > 0:
		return &InvalidInputError{Message: "batch does not accept --sha or --rev"}
	case flags.Target != "":
		return &InvalidInputError{Message: "batch does not accept --via"}
	default:
		return validateNoCallInputFlags(flags, "batch")
	}
}

func resolveBatchOutputMode(value string) (BatchFormat, error) {
	switch strings.TrimSpace(value) {
	case "", string(BatchFormatNDJSON):
		return BatchFormatNDJSON, nil
	case string(BatchFormatJSON):
		return BatchFormatJSON, nil
	default:
		return "", &InvalidInputError{Message: "batch output must be one of: json, ndjson"}
	}
}

func openBatchInput(stdin io.Reader, from string) (io.Reader, io.Closer, error) {
	if path := strings.TrimSpace(from); path != "" {
		file, err := os.Open(path)
		if err != nil {
			return nil, nil, fmt.Errorf("open batch input %s: %w", path, err)
		}
		return file, file, nil
	}

	file, ok := stdin.(*os.File)
	if ok && file != nil {
		info, err := file.Stat()
		if err == nil && (info.Mode()&os.ModeCharDevice) != 0 {
			return nil, nil, &InvalidInputError{Message: "batch requires --from or NDJSON on stdin"}
		}
	}

	return stdin, io.NopCloser(bytes.NewReader(nil)), nil
}

func executeBatchCommand(
	ctx context.Context,
	writer io.Writer,
	reader io.Reader,
	service Service,
	flags RootFlags,
	format BatchFormat,
) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024), 10*1024*1024)

	options := requestOptionsFromFlags(flags)
	items := make([]clioutput.BatchItem, 0)
	var (
		firstErr error
		failures int
		index    int
	)

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		envelope, err := decodeBatchEnvelope(line, flags.DryRun)
		if err != nil {
			return normalizeCLIValidation(err)
		}

		outputFormat, body, err := executeBatchEnvelope(ctx, service, envelope, options)
		item := clioutput.BatchItem{
			Index:   index,
			Request: envelope,
			OK:      err == nil,
		}
		if err != nil {
			item.Error = err.Error()
			if firstErr == nil {
				firstErr = err
			}
			failures++
		} else {
			item.Output = clioutput.NewBatchPayload(outputFormat, body)
		}

		if format == BatchFormatNDJSON {
			if err := clioutput.EncodeBatchItemNDJSON(writer, item); err != nil {
				return err
			}
		} else {
			items = append(items, item)
		}
		index++
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read batch input: %w", err)
	}

	if format == BatchFormatJSON {
		body, err := clioutput.RenderBatchItemsJSON(items)
		if err != nil {
			return err
		}
		if err := writeOutput(writer, body); err != nil {
			return err
		}
	}

	if firstErr != nil {
		return &BatchExecutionError{Err: firstErr, Count: failures}
	}
	return nil
}

func decodeBatchEnvelope(line []byte, batchDryRun bool) (request.Envelope, error) {
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()

	var envelope request.Envelope
	if err := decoder.Decode(&envelope); err != nil {
		return request.Envelope{}, &request.ValidationError{Message: fmt.Sprintf("invalid batch envelope: %v", err)}
	}

	switch envelope.Kind {
	case request.KindSpec, request.KindOperation:
		normalized, err := request.NormalizeEnvelope(envelope, request.NormalizeOptions{
			DefaultKind:      envelope.Kind,
			AllowMissingKind: false,
		})
		if err != nil {
			return request.Envelope{}, err
		}
		return normalized, nil
	case request.KindCall:
		envelope.DryRun = envelope.DryRun || batchDryRun
		return request.NormalizeCallEnvelope(envelope, request.NormalizeCallOptions{
			DefaultTarget:    request.DefaultShivaTarget,
			AllowMissingKind: false,
		})
	default:
		return request.Envelope{}, &request.ValidationError{Message: fmt.Sprintf("unsupported batch envelope kind %q", envelope.Kind)}
	}
}

func executeBatchEnvelope(
	ctx context.Context,
	service Service,
	envelope request.Envelope,
	options RequestOptions,
) (string, []byte, error) {
	switch envelope.Kind {
	case request.KindSpec:
		body, err := service.GetSpec(ctx, envelope, options, SpecFormatJSON)
		return string(SpecFormatJSON), body, err
	case request.KindOperation:
		body, err := service.GetOperation(ctx, envelope, options)
		return string(SpecFormatJSON), body, err
	case request.KindCall:
		body, err := service.ExecuteCall(ctx, envelope, options, CallFormatJSON)
		return string(CallFormatJSON), body, err
	default:
		return "", nil, &InvalidInputError{Message: fmt.Sprintf("unsupported batch envelope kind %q", envelope.Kind)}
	}
}
