package gitlab

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	defaultAPIPath    = "/api/v4"
	maxErrorBodyBytes = 4096
)

var ErrNotFound = errors.New("gitlab resource not found")

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type Option func(*Client)

type Client struct {
	baseURL    *url.URL
	token      string
	httpClient HTTPClient
}

type APIError struct {
	Method     string
	URL        string
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	message := strings.TrimSpace(e.Body)
	if message == "" {
		return fmt.Sprintf("gitlab api %s %s returned status %d", e.Method, e.URL, e.StatusCode)
	}
	return fmt.Sprintf("gitlab api %s %s returned status %d: %s", e.Method, e.URL, e.StatusCode, message)
}

type ChangedPath struct {
	OldPath     string
	NewPath     string
	NewFile     bool
	RenamedFile bool
	DeletedFile bool
}

func NewClient(baseURL, token string, options ...Option) (*Client, error) {
	normalizedBaseURL, err := normalizeBaseURL(baseURL)
	if err != nil {
		return nil, err
	}

	client := &Client{
		baseURL:    normalizedBaseURL,
		token:      strings.TrimSpace(token),
		httpClient: &http.Client{},
	}
	for _, option := range options {
		option(client)
	}
	if client.httpClient == nil {
		client.httpClient = &http.Client{}
	}

	return client, nil
}

func WithHTTPClient(httpClient HTTPClient) Option {
	return func(client *Client) {
		client.httpClient = httpClient
	}
}

func (c *Client) CompareChangedPaths(ctx context.Context, projectID int64, fromSHA, toSHA string) ([]ChangedPath, error) {
	if projectID < 1 {
		return nil, errors.New("project id must be positive")
	}
	if strings.TrimSpace(fromSHA) == "" {
		return nil, errors.New("from sha must not be empty")
	}
	if strings.TrimSpace(toSHA) == "" {
		return nil, errors.New("to sha must not be empty")
	}

	query := url.Values{}
	query.Set("from", fromSHA)
	query.Set("to", toSHA)

	requestURL := c.makeRequestURL(
		"/projects/"+strconv.FormatInt(projectID, 10)+"/repository/compare",
		"",
		query,
	)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build compare request: %w", err)
	}

	response, err := c.do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: project=%d compare from=%q to=%q", ErrNotFound, projectID, fromSHA, toSHA)
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, newAPIError(request, response)
	}

	var payload struct {
		Diffs []struct {
			OldPath     string `json:"old_path"`
			NewPath     string `json:"new_path"`
			NewFile     bool   `json:"new_file"`
			RenamedFile bool   `json:"renamed_file"`
			DeletedFile bool   `json:"deleted_file"`
		} `json:"diffs"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode compare response: %w", err)
	}

	changed := make([]ChangedPath, 0, len(payload.Diffs))
	for _, diff := range payload.Diffs {
		changed = append(changed, ChangedPath{
			OldPath:     diff.OldPath,
			NewPath:     diff.NewPath,
			NewFile:     diff.NewFile,
			RenamedFile: diff.RenamedFile,
			DeletedFile: diff.DeletedFile,
		})
	}

	return changed, nil
}

func (c *Client) GetFileContent(ctx context.Context, projectID int64, filePath, ref string) ([]byte, error) {
	if projectID < 1 {
		return nil, errors.New("project id must be positive")
	}
	normalizedPath := strings.TrimSpace(strings.TrimPrefix(filePath, "/"))
	if normalizedPath == "" {
		return nil, errors.New("file path must not be empty")
	}
	if strings.TrimSpace(ref) == "" {
		return nil, errors.New("ref must not be empty")
	}

	query := url.Values{}
	query.Set("ref", ref)

	requestURL := c.makeRequestURL(
		"/projects/"+strconv.FormatInt(projectID, 10)+"/repository/files/"+normalizedPath,
		"/projects/"+strconv.FormatInt(projectID, 10)+"/repository/files/"+url.PathEscape(normalizedPath),
		query,
	)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build file request: %w", err)
	}

	response, err := c.do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: project=%d path=%q ref=%q", ErrNotFound, projectID, normalizedPath, ref)
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, newAPIError(request, response)
	}

	var payload struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode repository file response: %w", err)
	}
	if strings.ToLower(payload.Encoding) != "base64" {
		return nil, fmt.Errorf("unsupported repository file encoding: %q", payload.Encoding)
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(payload.Content, "\n", ""))
	if err != nil {
		return nil, fmt.Errorf("decode repository file content: %w", err)
	}
	return decoded, nil
}

func normalizeBaseURL(rawBaseURL string) (*url.URL, error) {
	trimmed := strings.TrimSpace(rawBaseURL)
	if trimmed == "" {
		return nil, errors.New("gitlab base url must not be empty")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("parse gitlab base url: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("gitlab base url must include scheme and host")
	}

	basePath := strings.TrimSuffix(parsed.Path, "/")
	if basePath == "" {
		basePath = defaultAPIPath
	} else if !strings.HasSuffix(basePath, defaultAPIPath) {
		basePath += defaultAPIPath
	}
	parsed.Path = basePath
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed, nil
}

func (c *Client) makeRequestURL(pathSuffix string, rawPathSuffix string, query url.Values) string {
	endpoint := *c.baseURL
	basePath := strings.TrimSuffix(endpoint.Path, "/")
	if !strings.HasPrefix(pathSuffix, "/") {
		pathSuffix = "/" + pathSuffix
	}
	endpoint.Path = basePath + pathSuffix
	if rawPathSuffix != "" {
		if !strings.HasPrefix(rawPathSuffix, "/") {
			rawPathSuffix = "/" + rawPathSuffix
		}
		endpoint.RawPath = basePath + rawPathSuffix
	}
	endpoint.RawQuery = query.Encode()
	return endpoint.String()
}

func (c *Client) do(request *http.Request) (*http.Response, error) {
	request.Header.Set("Accept", "application/json")
	if c.token != "" {
		request.Header.Set("PRIVATE-TOKEN", c.token)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("gitlab request %s %s failed: %w", request.Method, request.URL.String(), err)
	}
	return response, nil
}

func newAPIError(request *http.Request, response *http.Response) error {
	limitedBody := io.LimitReader(response.Body, maxErrorBodyBytes)
	body, _ := io.ReadAll(limitedBody)
	return &APIError{
		Method:     request.Method,
		URL:        request.URL.String(),
		StatusCode: response.StatusCode,
		Body:       strings.TrimSpace(string(body)),
	}
}
