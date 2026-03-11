package cli

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBaseURL               = "http://127.0.0.1:8080"
	defaultRequestTimeoutSeconds = 10
)

type Config struct {
	BaseURL        string
	RequestTimeout time.Duration
}

func LoadConfigFromEnv() (Config, error) {
	cfg := Config{
		BaseURL:        envValue("SHIVA_BASE_URL", defaultBaseURL),
		RequestTimeout: time.Duration(defaultRequestTimeoutSeconds) * time.Second,
	}

	if rawTimeout, ok := os.LookupEnv("SHIVA_REQUEST_TIMEOUT_SECONDS"); ok {
		timeoutSeconds, err := strconv.Atoi(strings.TrimSpace(rawTimeout))
		if err != nil {
			return Config{}, &InvalidInputError{
				Message: fmt.Sprintf("invalid SHIVA_REQUEST_TIMEOUT_SECONDS: %v", err),
			}
		}
		if timeoutSeconds < 1 {
			return Config{}, &InvalidInputError{
				Message: "SHIVA_REQUEST_TIMEOUT_SECONDS must be at least 1",
			}
		}
		cfg.RequestTimeout = time.Duration(timeoutSeconds) * time.Second
	}

	parsedURL, err := url.Parse(strings.TrimSpace(cfg.BaseURL))
	if err != nil {
		return Config{}, &InvalidInputError{
			Message: fmt.Sprintf("invalid SHIVA_BASE_URL: %v", err),
		}
	}
	if parsedURL == nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return Config{}, &InvalidInputError{
			Message: "SHIVA_BASE_URL must be an absolute http(s) URL",
		}
	}
	if parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
		return Config{}, &InvalidInputError{
			Message: "SHIVA_BASE_URL must not include query or fragment components",
		}
	}
	if !strings.EqualFold(parsedURL.Scheme, "http") && !strings.EqualFold(parsedURL.Scheme, "https") {
		return Config{}, &InvalidInputError{
			Message: "SHIVA_BASE_URL must use http or https",
		}
	}

	cfg.BaseURL = strings.TrimRight(parsedURL.String(), "/")
	return cfg, nil
}

func envValue(key, fallback string) string {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}

	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

var errEmptyResponseBody = errors.New("empty response body")
