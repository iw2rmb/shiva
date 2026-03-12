package httpserver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/gofiber/fiber/v2"
)

type runtimeFailureClass string

const (
	runtimeFailureUnauthorized     runtimeFailureClass = "unauthorized"
	runtimeFailureForbidden        runtimeFailureClass = "forbidden"
	runtimeFailureNotFound         runtimeFailureClass = "not_found"
	runtimeFailureNotAcceptable    runtimeFailureClass = "not_acceptable"
	runtimeFailureUnsupportedMedia runtimeFailureClass = "unsupported_media"
	runtimeFailureUnprocessable    runtimeFailureClass = "unprocessable"
	runtimeFailureBadRequest       runtimeFailureClass = "bad_request"
)

type runtimeFailure struct {
	Class runtimeFailureClass
	Err   error
}

func (e *runtimeFailure) Error() string {
	if e == nil {
		return "runtime request failed"
	}
	if e.Err == nil {
		return string(e.Class)
	}
	return e.Err.Error()
}

func (e *runtimeFailure) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *runtimeFailure) preferredStatus() int {
	if e == nil {
		return http.StatusBadRequest
	}

	switch e.Class {
	case runtimeFailureUnauthorized:
		return http.StatusUnauthorized
	case runtimeFailureForbidden:
		return http.StatusForbidden
	case runtimeFailureNotFound:
		return http.StatusNotFound
	case runtimeFailureNotAcceptable:
		return http.StatusNotAcceptable
	case runtimeFailureUnsupportedMedia:
		return http.StatusUnsupportedMediaType
	case runtimeFailureUnprocessable:
		return http.StatusUnprocessableEntity
	default:
		return http.StatusBadRequest
	}
}

type runtimeValidatedRequest struct {
	HTTPRequest     *http.Request
	PathParams      map[string]string
	ValidationInput *openapi3filter.RequestValidationInput
}

var (
	errRuntimeUnauthorized = errors.New("runtime credential is required")
	errRuntimeForbidden    = errors.New("runtime credential is forbidden")
)

func validateRuntimeRequest(
	ctx context.Context,
	c *fiber.Ctx,
	resolved runtimeResolvedOperation,
) (runtimeValidatedRequest, error) {
	httpRequest, err := buildRuntimeHTTPRequest(c, resolved.Route.OpenAPIPath)
	if err != nil {
		return runtimeValidatedRequest{}, err
	}

	pathParams, err := extractRuntimePathParams(resolved.Candidate.Path, resolved.Route.OpenAPIPath)
	if err != nil {
		return runtimeValidatedRequest{}, err
	}

	validationInput := &openapi3filter.RequestValidationInput{
		Request:    httpRequest,
		PathParams: pathParams,
		Route: &routers.Route{
			Spec:      resolved.Document,
			Path:      resolved.Candidate.Path,
			PathItem:  resolved.PathItem,
			Method:    strings.ToUpper(resolved.Candidate.Method),
			Operation: resolved.Operation,
		},
		Options: &openapi3filter.Options{
			AuthenticationFunc: runtimeAuthenticationFunc,
		},
	}

	if err := openapi3filter.ValidateRequest(ctx, validationInput); err != nil {
		return runtimeValidatedRequest{}, classifyRuntimeFailure(err)
	}

	return runtimeValidatedRequest{
		HTTPRequest:     httpRequest,
		PathParams:      pathParams,
		ValidationInput: validationInput,
	}, nil
}

func buildRuntimeHTTPRequest(c *fiber.Ctx, openAPIPath string) (*http.Request, error) {
	body := append([]byte(nil), c.Body()...)
	target := openAPIPath
	if rawQuery := string(c.Request().URI().QueryString()); rawQuery != "" {
		target += "?" + rawQuery
	}

	bodyReader := bytes.NewReader(body)
	req, err := http.NewRequest(strings.ToUpper(c.Method()), target, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build runtime http request: %w", err)
	}
	req.Header = copyFiberHeaders(c)
	req.Host = string(c.Request().Host())
	req.RemoteAddr = c.IP()
	req.URL.RawPath = req.URL.EscapedPath()
	if len(body) == 0 {
		req.Body = http.NoBody
		req.GetBody = func() (io.ReadCloser, error) { return http.NoBody, nil }
		req.ContentLength = 0
		return req, nil
	}

	req.ContentLength = int64(len(body))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	req.Body, _ = req.GetBody()
	return req, nil
}

func copyFiberHeaders(c *fiber.Ctx) http.Header {
	headers := make(http.Header)
	c.Request().Header.VisitAll(func(key []byte, value []byte) {
		headers.Add(string(key), string(value))
	})
	return headers
}

func extractRuntimePathParams(templatePath string, actualPath string) (map[string]string, error) {
	templateSegments := splitHTTPPath(templatePath)
	actualSegments := splitHTTPPath(actualPath)
	if len(templateSegments) != len(actualSegments) {
		return nil, fmt.Errorf("runtime path params do not match template path=%q actual=%q", templatePath, actualPath)
	}

	params := make(map[string]string)
	for i := range templateSegments {
		templateSegment := templateSegments[i]
		actualSegment := actualSegments[i]

		if name, ok := pathParamName(templateSegment); ok {
			value, err := url.PathUnescape(actualSegment)
			if err != nil {
				return nil, fmt.Errorf("decode runtime path param %q: %w", name, err)
			}
			params[name] = value
			continue
		}
		if templateSegment != actualSegment {
			return nil, fmt.Errorf("runtime path segment mismatch template=%q actual=%q", templateSegment, actualSegment)
		}
	}

	return params, nil
}

func splitHTTPPath(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

func pathParamName(segment string) (string, bool) {
	if len(segment) < 3 {
		return "", false
	}
	if !strings.HasPrefix(segment, "{") || !strings.HasSuffix(segment, "}") {
		return "", false
	}
	name := strings.TrimSpace(segment[1 : len(segment)-1])
	if name == "" {
		return "", false
	}
	return name, true
}

func runtimeAuthenticationFunc(_ context.Context, input *openapi3filter.AuthenticationInput) error {
	if input == nil || input.RequestValidationInput == nil || input.RequestValidationInput.Request == nil {
		return fmt.Errorf("runtime authentication input is incomplete")
	}

	scheme := input.SecurityScheme
	if scheme == nil {
		return fmt.Errorf("runtime security scheme is not resolved")
	}

	switch scheme.Type {
	case "apiKey":
		return validateRuntimeAPIKey(input)
	case "http":
		return validateRuntimeHTTPSecurity(input)
	default:
		return fmt.Errorf("unsupported runtime security scheme type %q", scheme.Type)
	}
}

func validateRuntimeAPIKey(input *openapi3filter.AuthenticationInput) error {
	name := strings.TrimSpace(input.SecurityScheme.Name)
	if name == "" {
		return fmt.Errorf("runtime api key security scheme name must not be empty")
	}

	var found bool
	switch input.SecurityScheme.In {
	case "query":
		_, found = input.RequestValidationInput.GetQueryParams()[name]
	case "header":
		_, found = input.RequestValidationInput.Request.Header[http.CanonicalHeaderKey(name)]
	case "cookie":
		_, err := input.RequestValidationInput.Request.Cookie(name)
		found = !errors.Is(err, http.ErrNoCookie)
	default:
		return fmt.Errorf("unsupported runtime api key location %q", input.SecurityScheme.In)
	}

	if found {
		return nil
	}
	return input.NewError(fmt.Errorf("%w: %s not found in %s", errRuntimeUnauthorized, name, input.SecurityScheme.In))
}

func validateRuntimeHTTPSecurity(input *openapi3filter.AuthenticationInput) error {
	headerValue := strings.TrimSpace(input.RequestValidationInput.Request.Header.Get(fiber.HeaderAuthorization))
	switch strings.ToLower(strings.TrimSpace(input.SecurityScheme.Scheme)) {
	case "basic":
		if hasAuthorizationCredentials(headerValue, "Basic") {
			return nil
		}
		return input.NewError(fmt.Errorf("%w: basic authorization header is required", errRuntimeUnauthorized))
	case "bearer":
		if hasAuthorizationCredentials(headerValue, "Bearer") {
			return nil
		}
		return input.NewError(fmt.Errorf("%w: bearer authorization header is required", errRuntimeUnauthorized))
	default:
		return fmt.Errorf("unsupported runtime http security scheme %q", input.SecurityScheme.Scheme)
	}
}

func hasAuthorizationCredentials(value string, scheme string) bool {
	fields := strings.Fields(value)
	if len(fields) < 2 {
		return false
	}
	if !strings.EqualFold(fields[0], scheme) {
		return false
	}
	return strings.TrimSpace(strings.Join(fields[1:], " ")) != ""
}

func classifyRuntimeFailure(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errRuntimeUnauthorized):
		return &runtimeFailure{Class: runtimeFailureUnauthorized, Err: err}
	case errors.Is(err, errRuntimeForbidden):
		return &runtimeFailure{Class: runtimeFailureForbidden, Err: err}
	}

	converted := openapi3filter.ConvertErrors(err)
	var validationErr *openapi3filter.ValidationError
	if errors.As(converted, &validationErr) {
		return &runtimeFailure{
			Class: runtimeFailureClassFromStatus(validationErr.Status),
			Err:   converted,
		}
	}

	var requestErr *openapi3filter.RequestError
	if errors.As(err, &requestErr) {
		return &runtimeFailure{
			Class: runtimeFailureBadRequest,
			Err:   converted,
		}
	}

	return err
}

func runtimeFailureClassFromStatus(status int) runtimeFailureClass {
	switch status {
	case http.StatusUnauthorized:
		return runtimeFailureUnauthorized
	case http.StatusForbidden:
		return runtimeFailureForbidden
	case http.StatusNotFound:
		return runtimeFailureNotFound
	case http.StatusNotAcceptable:
		return runtimeFailureNotAcceptable
	case http.StatusUnsupportedMediaType:
		return runtimeFailureUnsupportedMedia
	case http.StatusUnprocessableEntity:
		return runtimeFailureUnprocessable
	default:
		return runtimeFailureBadRequest
	}
}

func sortedStringKeys[T any](items map[string]T) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
