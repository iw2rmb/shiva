package executor

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/iw2rmb/shiva/internal/cli/request"
)

type DispatchMode string

const (
	DispatchModeShiva  DispatchMode = "shiva"
	DispatchModeDirect DispatchMode = "direct"
)

type HTTPRequest struct {
	Method  string
	URL     string
	Headers map[string][]string
	Body    []byte
}

type DispatchPlan struct {
	Mode    DispatchMode  `json:"mode"`
	DryRun  bool          `json:"dry_run"`
	Network bool          `json:"network"`
	Request HTTPRequest   `json:"-"`
	Timeout time.Duration `json:"-"`
}

type CallPlan struct {
	Request  request.Envelope `json:"request"`
	Dispatch DispatchPlan     `json:"dispatch"`
}

type HTTPResponse struct {
	StatusCode int
	Headers    map[string][]string
	Body       []byte
}

var pathParameterPattern = regexp.MustCompile(`\{([^{}]+)\}`)

func PlanShivaDispatchCall(input request.Envelope, baseURL string, token string, timeout time.Duration) (CallPlan, error) {
	if strings.TrimSpace(baseURL) == "" {
		return CallPlan{}, fmt.Errorf("base url must not be empty")
	}
	if timeout <= 0 {
		return CallPlan{}, fmt.Errorf("request timeout must be greater than zero")
	}

	envelope, err := request.NormalizeResolvedCallEnvelope(input, request.DefaultShivaTarget)
	if err != nil {
		return CallPlan{}, err
	}

	body, err := json.Marshal(envelope)
	if err != nil {
		return CallPlan{}, fmt.Errorf("encode shiva call envelope: %w", err)
	}

	headers := map[string][]string{
		"Content-Type": []string{"application/json"},
	}
	if strings.TrimSpace(token) != "" {
		headers["Authorization"] = []string{"Bearer " + strings.TrimSpace(token)}
	}

	return CallPlan{
		Request: envelope,
		Dispatch: DispatchPlan{
			Mode:    DispatchModeShiva,
			DryRun:  envelope.DryRun,
			Network: !envelope.DryRun,
			Request: HTTPRequest{
				Method:  "POST",
				URL:     strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/v1/call",
				Headers: headers,
				Body:    body,
			},
			Timeout: timeout,
		},
	}, nil
}

func PlanShivaCall(input request.Envelope) (CallPlan, error) {
	envelope, err := request.NormalizeResolvedCallEnvelope(input, request.DefaultShivaTarget)
	if err != nil {
		return CallPlan{}, err
	}

	return CallPlan{
		Request: envelope,
		Dispatch: DispatchPlan{
			Mode:    DispatchModeShiva,
			DryRun:  envelope.DryRun,
			Network: false,
		},
	}, nil
}

func PlanDirectCall(input request.Envelope, baseURL string, token string, timeout time.Duration) (CallPlan, error) {
	if strings.TrimSpace(baseURL) == "" {
		return CallPlan{}, fmt.Errorf("base url must not be empty")
	}
	if timeout <= 0 {
		return CallPlan{}, fmt.Errorf("request timeout must be greater than zero")
	}

	envelope, err := request.NormalizeResolvedCallEnvelope(input, strings.TrimSpace(input.Target))
	if err != nil {
		return CallPlan{}, err
	}
	if strings.TrimSpace(envelope.Target) == "" {
		return CallPlan{}, fmt.Errorf("target must not be empty")
	}

	resolvedPath, err := interpolatePath(envelope.Path, envelope.PathParams)
	if err != nil {
		return CallPlan{}, err
	}
	resolvedURL, err := joinURL(baseURL, resolvedPath, envelope.QueryParams)
	if err != nil {
		return CallPlan{}, err
	}

	headers := cloneHeaders(envelope.Headers)
	if headers == nil {
		headers = make(map[string][]string)
	}
	if len(envelope.JSONBody) > 0 && !hasHeader(headers, "Content-Type") {
		headers["Content-Type"] = []string{"application/json"}
	}
	if strings.TrimSpace(token) != "" && !hasHeader(headers, "Authorization") {
		headers["Authorization"] = []string{"Bearer " + strings.TrimSpace(token)}
	}

	body := []byte(envelope.Body)
	if len(envelope.JSONBody) > 0 {
		body = append([]byte(nil), envelope.JSONBody...)
	}

	return CallPlan{
		Request: envelope,
		Dispatch: DispatchPlan{
			Mode:    DispatchModeDirect,
			DryRun:  envelope.DryRun,
			Network: !envelope.DryRun,
			Request: HTTPRequest{
				Method:  strings.ToUpper(envelope.Method),
				URL:     resolvedURL,
				Headers: headers,
				Body:    body,
			},
			Timeout: timeout,
		},
	}, nil
}

func interpolatePath(path string, pathParams map[string]string) (string, error) {
	used := make(map[string]struct{})
	resolved := pathParameterPattern.ReplaceAllStringFunc(path, func(raw string) string {
		name := pathParameterPattern.ReplaceAllString(raw, "$1")
		used[name] = struct{}{}
		value, ok := pathParams[name]
		if !ok {
			return ""
		}
		return url.PathEscape(value)
	})

	for _, match := range pathParameterPattern.FindAllStringSubmatch(path, -1) {
		name := match[1]
		if _, ok := pathParams[name]; !ok {
			return "", fmt.Errorf("path parameter %q is required", name)
		}
	}
	for key := range pathParams {
		if _, ok := used[key]; !ok {
			return "", fmt.Errorf("path parameter %q is not used by %s", key, path)
		}
	}
	return resolved, nil
}

func joinURL(baseURL string, path string, queryParams map[string][]string) (string, error) {
	parsedBaseURL, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", fmt.Errorf("parse base url: %w", err)
	}
	resolvedURL, err := parsedBaseURL.Parse(path)
	if err != nil {
		return "", fmt.Errorf("resolve request url: %w", err)
	}
	query := resolvedURL.Query()
	for key, values := range queryParams {
		for _, value := range values {
			query.Add(key, value)
		}
	}
	resolvedURL.RawQuery = query.Encode()
	return resolvedURL.String(), nil
}

func hasHeader(headers map[string][]string, key string) bool {
	for candidate := range headers {
		if strings.EqualFold(candidate, key) {
			return true
		}
	}
	return false
}

func cloneHeaders(input map[string][]string) map[string][]string {
	if len(input) == 0 {
		return nil
	}

	output := make(map[string][]string, len(input))
	for key, values := range input {
		copied := make([]string, len(values))
		copy(copied, values)
		output[key] = copied
	}
	return output
}
