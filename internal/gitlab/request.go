package gitlab

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	defaultHTTPTimeout         = 30 * time.Second
	defaultMaxRetries          = 5
	defaultNon4294xxRetryCap   = 1
	defaultBackoffBase         = 500 * time.Millisecond
	defaultBackoffMax          = 30 * time.Second
	defaultInstanceConcurrency = 1
	defaultInstanceMinInterval = 0
)

type requestStatusError struct {
	StatusCode int
	Body       string
	RetryAfter string
}

func (e *requestStatusError) apiError(request *http.Request) error {
	return &APIError{
		Method:     request.Method,
		URL:        request.URL.String(),
		StatusCode: e.StatusCode,
		Body:       e.Body,
	}
}

func (c *Client) do(request *http.Request) (*http.Response, *requestStatusError, error) {
	non4294xxRetries := 0
	for attempt := 0; ; attempt++ {
		response, statusErr, err := c.doOnce(request)
		if err == nil && statusErr == nil {
			return response, nil, nil
		}
		if !c.shouldRetry(err, statusErr, attempt, &non4294xxRetries) {
			if err != nil {
				return nil, nil, fmt.Errorf("gitlab request %s %s failed: %w", request.Method, request.URL.String(), err)
			}
			return nil, statusErr, nil
		}

		backoff := c.backoff(attempt + 1)
		if statusErr != nil && statusErr.StatusCode == http.StatusTooManyRequests {
			if retryAfter, ok := parseRetryAfter(statusErr.RetryAfter, c.now()); ok && retryAfter > 0 {
				backoff = retryAfter
			}
		}
		if sleepErr := c.sleep(request.Context(), backoff); sleepErr != nil {
			return nil, nil, sleepErr
		}
	}
}

func (c *Client) doOnce(request *http.Request) (*http.Response, *requestStatusError, error) {
	requestCtx := request.Context()
	cancel := func() {}
	if c.timeout > 0 {
		requestCtx, cancel = context.WithTimeout(request.Context(), c.timeout)
	}
	defer cancel()

	release, err := c.limiter.Acquire(requestCtx, request.URL.Host)
	if err != nil {
		return nil, nil, err
	}
	defer release()

	attempt := request.Clone(requestCtx)
	attempt.Header = request.Header.Clone()
	if attempt.Header == nil {
		attempt.Header = make(http.Header)
	}
	attempt.Header.Set("Accept", "application/json")
	if c.token != "" {
		attempt.Header.Set("PRIVATE-TOKEN", c.token)
	}

	response, err := c.httpClient.Do(attempt)
	if err != nil {
		return nil, nil, err
	}
	if response.StatusCode >= 200 && response.StatusCode <= 299 {
		return response, nil, nil
	}

	body, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBodyBytes))
	_ = response.Body.Close()
	return nil, &requestStatusError{
		StatusCode: response.StatusCode,
		Body:       strings.TrimSpace(string(body)),
		RetryAfter: strings.TrimSpace(response.Header.Get("Retry-After")),
	}, nil
}

func (c *Client) shouldRetry(reqErr error, statusErr *requestStatusError, attempt int, non4294xxRetries *int) bool {
	if attempt >= c.maxRetries {
		return false
	}
	if reqErr != nil {
		return true
	}
	if statusErr == nil {
		return false
	}
	if statusErr.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if statusErr.StatusCode >= 500 {
		return true
	}
	if statusErr.StatusCode >= 400 && statusErr.StatusCode < 500 {
		if *non4294xxRetries >= c.non4294xxRetryCap {
			return false
		}
		*non4294xxRetries++
		return true
	}
	return false
}

func (c *Client) backoff(retryAttempt int) time.Duration {
	d := c.backoffBase
	for i := 1; i < retryAttempt; i++ {
		if d >= c.backoffMax {
			return c.backoffMax
		}
		d *= 2
	}
	if d > c.backoffMax {
		return c.backoffMax
	}
	return d
}

func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds <= 0 {
			return 0, false
		}
		return time.Duration(seconds) * time.Second, true
	}
	if at, err := http.ParseTime(value); err == nil {
		d := at.Sub(now)
		if d <= 0 {
			return 0, false
		}
		return d, true
	}
	return 0, false
}

func withTimeout(timeout time.Duration) Option {
	return func(client *Client) {
		client.timeout = timeout
	}
}

func withRetrySettings(maxRetries, non4294xxRetryCap int, backoffBase, backoffMax time.Duration) Option {
	return func(client *Client) {
		client.maxRetries = maxRetries
		client.non4294xxRetryCap = non4294xxRetryCap
		client.backoffBase = backoffBase
		client.backoffMax = backoffMax
	}
}

func withLimiterSettings(concurrency int, minInterval time.Duration) Option {
	return func(client *Client) {
		client.instanceConcurrency = concurrency
		client.instanceMinInterval = minInterval
	}
}

func withNow(now func() time.Time) Option {
	return func(client *Client) {
		client.now = now
	}
}

func withSleep(sleep func(context.Context, time.Duration) error) Option {
	return func(client *Client) {
		client.sleep = sleep
	}
}
