package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/iw2rmb/shiva/internal/cli/request"
)

type SpecFormat string

const (
	SpecFormatJSON SpecFormat = "json"
	SpecFormatYAML SpecFormat = "yaml"
)

type HTTPClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewHTTPClient(cfg Config) (*HTTPClient, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, &InvalidInputError{Message: "base url must not be empty"}
	}
	if cfg.RequestTimeout <= 0 {
		return nil, &InvalidInputError{Message: "request timeout must be greater than zero"}
	}

	return &HTTPClient{
		baseURL: cfg.BaseURL,
		httpClient: &http.Client{
			Timeout: cfg.RequestTimeout,
		},
	}, nil
}

func (c *HTTPClient) GetSpec(ctx context.Context, selector request.Envelope, format SpecFormat) ([]byte, error) {
	if strings.TrimSpace(selector.Repo) == "" {
		return nil, &InvalidInputError{Message: "repo path must not be empty"}
	}
	if format != SpecFormatJSON && format != SpecFormatYAML {
		return nil, &InvalidInputError{Message: fmt.Sprintf("unsupported spec format %q", format)}
	}

	query := snapshotQuery(selector)
	query.Set("format", string(format))

	return c.get(ctx, "/v1/spec?"+query.Encode())
}

func (c *HTTPClient) GetOperation(ctx context.Context, selector request.Envelope) ([]byte, error) {
	if strings.TrimSpace(selector.Repo) == "" {
		return nil, &InvalidInputError{Message: "repo path must not be empty"}
	}

	query := snapshotQuery(selector)
	if strings.TrimSpace(selector.OperationID) != "" {
		query.Set("operation_id", selector.OperationID)
	} else {
		query.Set("method", selector.Method)
		query.Set("path", selector.Path)
	}

	return c.get(ctx, "/v1/operation?"+query.Encode())
}

func (c *HTTPClient) PlanCall(ctx context.Context, selector request.Envelope) ([]byte, error) {
	body, err := json.Marshal(selector)
	if err != nil {
		return nil, fmt.Errorf("encode call request: %w", err)
	}

	return c.postJSON(ctx, "/v1/call", body)
}

func (c *HTTPClient) Health(ctx context.Context) ([]byte, error) {
	return c.get(ctx, "/healthz")
}

func snapshotQuery(selector request.Envelope) url.Values {
	query := url.Values{}
	query.Set("repo", selector.Repo)
	if strings.TrimSpace(selector.API) != "" {
		query.Set("api", selector.API)
	}
	if selector.RevisionID > 0 {
		query.Set("revision_id", strconv.FormatInt(selector.RevisionID, 10))
	}
	if strings.TrimSpace(selector.SHA) != "" {
		query.Set("sha", selector.SHA)
	}
	return query
}

func (c *HTTPClient) get(ctx context.Context, requestPath string) ([]byte, error) {
	if c == nil || c.httpClient == nil {
		return nil, fmt.Errorf("http client is not configured")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+requestPath, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &TransportError{Err: err}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, &HTTPError{
			StatusCode: resp.StatusCode,
			Message:    decodeErrorMessage(resp.StatusCode, body),
		}
	}
	if len(body) == 0 {
		return nil, errEmptyResponseBody
	}

	return body, nil
}

func (c *HTTPClient) postJSON(ctx context.Context, requestPath string, body []byte) ([]byte, error) {
	if c == nil || c.httpClient == nil {
		return nil, fmt.Errorf("http client is not configured")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+requestPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &TransportError{Err: err}
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, &HTTPError{
			StatusCode: resp.StatusCode,
			Message:    decodeErrorMessage(resp.StatusCode, responseBody),
		}
	}
	if len(responseBody) == 0 {
		return nil, errEmptyResponseBody
	}

	return responseBody, nil
}

func decodeErrorMessage(statusCode int, body []byte) string {
	trimmedBody := strings.TrimSpace(string(body))
	if trimmedBody == "" {
		return fmt.Sprintf("request failed with status %d", statusCode)
	}

	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && strings.TrimSpace(payload.Error) != "" {
		return payload.Error
	}

	return trimmedBody
}
