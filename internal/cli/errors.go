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
	Message    string
	Candidates []APICandidate
}

type APICandidate struct {
	API    string
	Status string
}

func (c APICandidate) String() string {
	if strings.TrimSpace(c.Status) == "" {
		return c.API
	}
	return fmt.Sprintf("%s (%s)", c.API, c.Status)
}

func (e *AmbiguousAPIError) Error() string {
	if e == nil {
		return "multiple active apis matched"
	}

	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = "multiple APIs matched the selector"
	}
	if len(e.Candidates) == 0 {
		return message
	}
	return message + "\n" + formatCandidates(e.Candidates)
}

type OperationCandidate struct {
	API         string
	Method      string
	Path        string
	OperationID string
}

func (c OperationCandidate) String() string {
	parts := make([]string, 0, 2)
	if strings.TrimSpace(c.API) != "" {
		parts = append(parts, c.API)
	}
	parts = append(parts, strings.TrimSpace(c.Method)+" "+strings.TrimSpace(c.Path))
	if strings.TrimSpace(c.OperationID) != "" {
		parts = append(parts, "operation_id="+c.OperationID)
	}
	return strings.Join(parts, " ")
}

type AmbiguousOperationError struct {
	Message    string
	Candidates []OperationCandidate
}

func (e *AmbiguousOperationError) Error() string {
	if e == nil {
		return "operation id is ambiguous"
	}

	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = "operation selector matched multiple operations"
	}
	if len(e.Candidates) == 0 {
		return message
	}
	return message + "\n" + formatCandidates(e.Candidates)
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
