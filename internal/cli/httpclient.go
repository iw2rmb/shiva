package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type SpecFormat string

const (
	SpecFormatJSON SpecFormat = "json"
	SpecFormatYAML SpecFormat = "yaml"
)

type APISpecListing struct {
	API    string `json:"api"`
	Status string `json:"status"`
}

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

func (c *HTTPClient) ListAPISpecs(ctx context.Context, repoPath string) ([]APISpecListing, error) {
	query := url.Values{}
	query.Set("repo", repoPath)

	body, err := c.get(ctx, "/v1/apis?"+query.Encode())
	if err != nil {
		return nil, err
	}

	var listings []APISpecListing
	if err := json.Unmarshal(body, &listings); err != nil {
		return nil, fmt.Errorf("decode api listing response: %w", err)
	}
	return listings, nil
}

func (c *HTTPClient) GetSpec(ctx context.Context, repoPath string, apiRoot string, format SpecFormat) ([]byte, error) {
	if strings.TrimSpace(repoPath) == "" {
		return nil, &InvalidInputError{Message: "repo path must not be empty"}
	}
	if format != SpecFormatJSON && format != SpecFormatYAML {
		return nil, &InvalidInputError{Message: fmt.Sprintf("unsupported spec format %q", format)}
	}

	query := url.Values{}
	query.Set("repo", repoPath)
	if strings.TrimSpace(apiRoot) != "" {
		query.Set("api", apiRoot)
	}
	query.Set("format", string(format))

	return c.get(ctx, "/v1/spec?"+query.Encode())
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
