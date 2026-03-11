package cli

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const (
	ExitCodeSuccess       = 0
	ExitCodeInvalidInput  = 2
	ExitCodeNotFound      = 3
	ExitCodeConflict      = 4
	ExitCodeTransport     = 10
	ExitCodeInternalError = 11
)

type InvalidInputError struct {
	Message string
}

func (e *InvalidInputError) Error() string {
	if e == nil || strings.TrimSpace(e.Message) == "" {
		return "invalid input"
	}
	return e.Message
}

type NotFoundError struct {
	Message string
}

func (e *NotFoundError) Error() string {
	if e == nil || strings.TrimSpace(e.Message) == "" {
		return "not found"
	}
	return e.Message
}

type AmbiguousAPIError struct {
	Repo string
	APIs []string
}

func (e *AmbiguousAPIError) Error() string {
	if e == nil {
		return "multiple active apis matched"
	}
	return fmt.Sprintf(
		"repo %q has multiple active apis; draft CLI requires exactly one: %s",
		e.Repo,
		strings.Join(e.APIs, ", "),
	)
}

type OperationCandidate struct {
	Method string
	Path   string
}

type AmbiguousOperationError struct {
	Repo        string
	OperationID string
	Candidates  []OperationCandidate
}

func (e *AmbiguousOperationError) Error() string {
	if e == nil {
		return "operation id is ambiguous"
	}

	parts := make([]string, 0, len(e.Candidates))
	for _, candidate := range e.Candidates {
		parts = append(parts, fmt.Sprintf("%s %s", candidate.Method, candidate.Path))
	}

	return fmt.Sprintf(
		"operation %q in repo %q matched multiple endpoints: %s",
		e.OperationID,
		e.Repo,
		strings.Join(parts, ", "),
	)
}

type HTTPError struct {
	StatusCode int
	Message    string
}

func (e *HTTPError) Error() string {
	if e == nil {
		return "http request failed"
	}
	if strings.TrimSpace(e.Message) == "" {
		return fmt.Sprintf("http request failed with status %d", e.StatusCode)
	}
	return e.Message
}

type TransportError struct {
	Err error
}

func (e *TransportError) Error() string {
	if e == nil || e.Err == nil {
		return "transport error"
	}
	return fmt.Sprintf("transport error: %v", e.Err)
}

func (e *TransportError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func ExitCode(err error) int {
	if err == nil {
		return ExitCodeSuccess
	}

	var invalidInputErr *InvalidInputError
	if errors.As(err, &invalidInputErr) {
		return ExitCodeInvalidInput
	}

	var ambiguousAPIErr *AmbiguousAPIError
	if errors.As(err, &ambiguousAPIErr) {
		return ExitCodeInvalidInput
	}

	var ambiguousOperationErr *AmbiguousOperationError
	if errors.As(err, &ambiguousOperationErr) {
		return ExitCodeInvalidInput
	}

	var notFoundErr *NotFoundError
	if errors.As(err, &notFoundErr) {
		return ExitCodeNotFound
	}

	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		switch httpErr.StatusCode {
		case 404:
			return ExitCodeNotFound
		case 409:
			return ExitCodeConflict
		default:
			return ExitCodeInternalError
		}
	}

	var transportErr *TransportError
	if errors.As(err, &transportErr) {
		return ExitCodeTransport
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return ExitCodeTransport
	}

	return ExitCodeInternalError
}
