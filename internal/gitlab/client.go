package gitlab

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
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
	baseURL             *url.URL
	token               string
	httpClient          HTTPClient
	timeout             time.Duration
	maxRetries          int
	non4294xxRetryCap   int
	backoffBase         time.Duration
	backoffMax          time.Duration
	instanceConcurrency int
	instanceMinInterval time.Duration
	now                 func() time.Time
	sleep               func(context.Context, time.Duration) error
	limiter             *instanceLimiterSet
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

type TreeEntry struct {
	Path string
	Type string
}

func NewClient(baseURL, token string, options ...Option) (*Client, error) {
	normalizedBaseURL, err := normalizeBaseURL(baseURL)
	if err != nil {
		return nil, err
	}

	client := &Client{
		baseURL:             normalizedBaseURL,
		token:               strings.TrimSpace(token),
		httpClient:          &http.Client{},
		timeout:             defaultHTTPTimeout,
		maxRetries:          defaultMaxRetries,
		non4294xxRetryCap:   defaultNon4294xxRetryCap,
		backoffBase:         defaultBackoffBase,
		backoffMax:          defaultBackoffMax,
		instanceConcurrency: defaultInstanceConcurrency,
		instanceMinInterval: defaultInstanceMinInterval,
		now: func() time.Time {
			return time.Now().UTC()
		},
		sleep: sleepContext,
	}
	for _, option := range options {
		option(client)
	}
	if client.httpClient == nil {
		client.httpClient = &http.Client{}
	}
	if client.timeout <= 0 {
		client.timeout = defaultHTTPTimeout
	}
	if client.maxRetries <= 0 {
		client.maxRetries = defaultMaxRetries
	}
	if client.non4294xxRetryCap <= 0 {
		client.non4294xxRetryCap = defaultNon4294xxRetryCap
	}
	if client.backoffBase <= 0 {
		client.backoffBase = defaultBackoffBase
	}
	if client.backoffMax <= 0 {
		client.backoffMax = defaultBackoffMax
	}
	if client.instanceConcurrency <= 0 {
		client.instanceConcurrency = defaultInstanceConcurrency
	}
	if client.instanceMinInterval < 0 {
		client.instanceMinInterval = defaultInstanceMinInterval
	}
	if client.now == nil {
		client.now = func() time.Time {
			return time.Now().UTC()
		}
	}
	if client.sleep == nil {
		client.sleep = sleepContext
	}
	client.limiter = newInstanceLimiterSet(instanceLimiterSetOptions{
		Concurrency: client.instanceConcurrency,
		MinInterval: client.instanceMinInterval,
		Now:         client.now,
		Sleep:       client.sleep,
	})

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

	response, statusErr, err := c.do(request)
	if err != nil {
		return nil, err
	}
	if statusErr != nil {
		if statusErr.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("%w: project=%d compare from=%q to=%q", ErrNotFound, projectID, fromSHA, toSHA)
		}
		return nil, statusErr.apiError(request)
	}
	defer response.Body.Close()

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

	response, statusErr, err := c.do(request)
	if err != nil {
		return nil, err
	}
	if statusErr != nil {
		if statusErr.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("%w: project=%d path=%q ref=%q", ErrNotFound, projectID, normalizedPath, ref)
		}
		return nil, statusErr.apiError(request)
	}
	defer response.Body.Close()

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

func (c *Client) ListRepositoryTree(ctx context.Context, projectID int64, sha, path string, recursive bool) ([]TreeEntry, error) {
	if projectID < 1 {
		return nil, errors.New("project id must be positive")
	}
	normalizedSHA := strings.TrimSpace(sha)
	if normalizedSHA == "" {
		return nil, errors.New("sha must not be empty")
	}
	normalizedPath := normalizeRepositoryPath(path)

	query := url.Values{}
	query.Set("ref", normalizedSHA)
	query.Set("recursive", strconv.FormatBool(recursive))
	query.Set("path", normalizedPath)
	query.Set("page", "1")

	entries := make([]TreeEntry, 0)
	for {
		requestURL := c.makeRequestURL(
			"/projects/"+strconv.FormatInt(projectID, 10)+"/repository/tree",
			"",
			query,
		)
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if err != nil {
			return nil, fmt.Errorf("build repository tree request: %w", err)
		}

		response, statusErr, err := c.do(request)
		if err != nil {
			return nil, err
		}
		if statusErr != nil {
			if statusErr.StatusCode == http.StatusNotFound {
				return nil, fmt.Errorf("%w: project=%d sha=%q path=%q", ErrNotFound, projectID, normalizedSHA, normalizedPath)
			}
			return nil, statusErr.apiError(request)
		}

		var payload []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		}
		if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
			response.Body.Close()
			return nil, fmt.Errorf("decode repository tree response: %w", err)
		}
		response.Body.Close()

		for _, item := range payload {
			normalizedType := normalizeTreeNodeType(item.Type)
			if normalizedType != "file" {
				continue
			}
			entries = append(entries, TreeEntry{
				Path: normalizeRepositoryPath(item.Path),
				Type: normalizedType,
			})
		}

		nextPage := strings.TrimSpace(response.Header.Get("X-Next-Page"))
		if nextPage == "" {
			break
		}
		query.Set("page", nextPage)
	}

	return entries, nil
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

func normalizeRepositoryPath(rawPath string) string {
	return strings.TrimPrefix(strings.TrimSpace(rawPath), "/")
}

func normalizeTreeNodeType(rawType string) string {
	switch strings.ToLower(strings.TrimSpace(rawType)) {
	case "blob":
		return "file"
	default:
		return strings.ToLower(strings.TrimSpace(rawType))
	}
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
