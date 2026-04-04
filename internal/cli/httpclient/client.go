package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/iw2rmb/shiva/internal/cli/request"
	"github.com/iw2rmb/shiva/internal/repoid"
)

type Config struct {
	BaseURL        string
	RequestTimeout time.Duration
	Token          string
}

type SpecFormat string

const (
	SpecFormatJSON SpecFormat = "json"
	SpecFormatYAML SpecFormat = "yaml"
)

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

var errEmptyResponseBody = errors.New("empty response body")

func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("base url must not be empty")
	}
	if cfg.RequestTimeout <= 0 {
		return nil, fmt.Errorf("request timeout must be greater than zero")
	}

	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		token:   strings.TrimSpace(cfg.Token),
		httpClient: &http.Client{
			Timeout: cfg.RequestTimeout,
		},
	}, nil
}

func (c *Client) GetSpec(ctx context.Context, selector request.Envelope, format SpecFormat) ([]byte, error) {
	if strings.TrimSpace(selector.Namespace) == "" {
		return nil, fmt.Errorf("namespace must not be empty")
	}
	if strings.TrimSpace(selector.Repo) == "" {
		return nil, fmt.Errorf("repo must not be empty")
	}
	if format != SpecFormatJSON && format != SpecFormatYAML {
		return nil, fmt.Errorf("unsupported spec format %q", format)
	}

	query := snapshotQuery(selector)
	query.Set("format", string(format))

	return c.get(ctx, "/v1/spec?"+query.Encode())
}

func (c *Client) GetOperation(ctx context.Context, selector request.Envelope) ([]byte, error) {
	if strings.TrimSpace(selector.Namespace) == "" {
		return nil, fmt.Errorf("namespace must not be empty")
	}
	if strings.TrimSpace(selector.Repo) == "" {
		return nil, fmt.Errorf("repo must not be empty")
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

func (c *Client) ListRepos(ctx context.Context) ([]byte, error) {
	return c.get(ctx, "/v1/repos")
}

func (c *Client) CountNamespaces(ctx context.Context) ([]byte, error) {
	return c.get(ctx, "/v1/namespaces/count")
}

func (c *Client) ListNamespaces(ctx context.Context) ([]byte, error) {
	return c.get(ctx, "/v1/namespaces")
}

func (c *Client) GetCatalogStatus(ctx context.Context, repo string) ([]byte, error) {
	identity, err := repoid.ParsePath(repo)
	if err != nil {
		return nil, fmt.Errorf("repo path must be <namespace>/<repo>")
	}

	query := url.Values{}
	query.Set("namespace", identity.Namespace)
	query.Set("repo", identity.Repo)
	return c.get(ctx, "/v1/catalog/status?"+query.Encode())
}

func (c *Client) ListAPIs(ctx context.Context, selector request.Envelope) ([]byte, error) {
	query := snapshotQuery(selector)
	return c.get(ctx, "/v1/apis?"+query.Encode())
}

func (c *Client) ListOperations(ctx context.Context, selector request.Envelope) ([]byte, error) {
	query := snapshotQuery(selector)
	return c.get(ctx, "/v1/operations?"+query.Encode())
}

func (c *Client) Health(ctx context.Context) ([]byte, error) {
	return c.get(ctx, "/healthz")
}

func snapshotQuery(selector request.Envelope) url.Values {
	query := url.Values{}
	query.Set("namespace", selector.Namespace)
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

func (c *Client) get(ctx context.Context, requestPath string) ([]byte, error) {
	if c == nil || c.httpClient == nil {
		return nil, fmt.Errorf("http client is not configured")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+requestPath, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	c.applyHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
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
			Body:       append([]byte(nil), body...),
		}
	}
	if len(body) == 0 {
		return nil, errEmptyResponseBody
	}

	return body, nil
}

func (c *Client) postJSON(ctx context.Context, requestPath string, body []byte) ([]byte, error) {
	if c == nil || c.httpClient == nil {
		return nil, fmt.Errorf("http client is not configured")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+requestPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.applyHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
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
			Body:       append([]byte(nil), responseBody...),
		}
	}
	if len(responseBody) == 0 {
		return nil, errEmptyResponseBody
	}

	return responseBody, nil
}

func (c *Client) applyHeaders(req *http.Request) {
	if c == nil || req == nil {
		return
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

type HTTPError struct {
	StatusCode int
	Message    string
	Body       []byte
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
