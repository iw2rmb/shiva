package profile

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

const DefaultName = "default"

type Source struct {
	Name      string
	BaseURL   string
	Token     string
	TokenEnv  string
	Timeout   time.Duration
	ConfigRef string
}

func Normalize(name string, source Source) (Source, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Source{}, fmt.Errorf("profile name must not be empty")
	}

	baseURL, err := normalizeBaseURL(source.BaseURL)
	if err != nil {
		return Source{}, fmt.Errorf("profile %q: %w", name, err)
	}

	timeout := source.Timeout
	if timeout <= 0 {
		return Source{}, fmt.Errorf("profile %q: timeout must be greater than zero", name)
	}

	return Source{
		Name:      name,
		BaseURL:   baseURL,
		Token:     strings.TrimSpace(source.Token),
		TokenEnv:  strings.TrimSpace(source.TokenEnv),
		Timeout:   timeout,
		ConfigRef: strings.TrimSpace(source.ConfigRef),
	}, nil
}

func (s Source) ResolvedToken() string {
	if token := strings.TrimSpace(s.Token); token != "" {
		return token
	}
	if envKey := strings.TrimSpace(s.TokenEnv); envKey != "" {
		return strings.TrimSpace(os.Getenv(envKey))
	}
	return ""
}

func normalizeBaseURL(raw string) (string, error) {
	parsedURL, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("invalid base_url: %w", err)
	}
	if parsedURL == nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return "", fmt.Errorf("base_url must be an absolute http(s) URL")
	}
	if parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
		return "", fmt.Errorf("base_url must not include query or fragment components")
	}
	if !strings.EqualFold(parsedURL.Scheme, "http") && !strings.EqualFold(parsedURL.Scheme, "https") {
		return "", fmt.Errorf("base_url must use http or https")
	}
	return strings.TrimRight(parsedURL.String(), "/"), nil
}
