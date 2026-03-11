package target

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

type Mode string

const (
	ModeDirect Mode = "direct"
	ModeShiva  Mode = "shiva"

	BuiltinShivaName = "shiva"
)

type Entry struct {
	Name          string
	Mode          Mode
	SourceProfile string
	BaseURL       string
	Token         string
	TokenEnv      string
	Timeout       time.Duration
	ConfigRef     string
}

func BuiltinShiva() Entry {
	return Entry{
		Name: BuiltinShivaName,
		Mode: ModeShiva,
	}
}

func Normalize(name string, entry Entry) (Entry, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Entry{}, fmt.Errorf("target name must not be empty")
	}

	mode := Mode(strings.ToLower(strings.TrimSpace(string(entry.Mode))))
	if mode != ModeDirect && mode != ModeShiva {
		return Entry{}, fmt.Errorf("target %q: mode must be one of: %s, %s", name, ModeDirect, ModeShiva)
	}

	baseURL := strings.TrimSpace(entry.BaseURL)
	timeout := entry.Timeout
	if mode == ModeDirect {
		if timeout <= 0 {
			return Entry{}, fmt.Errorf("target %q: timeout must be greater than zero", name)
		}

		normalizedURL, err := normalizeBaseURL(baseURL)
		if err != nil {
			return Entry{}, fmt.Errorf("target %q: %w", name, err)
		}
		baseURL = normalizedURL
	} else {
		baseURL = ""
		timeout = 0
	}

	return Entry{
		Name:          name,
		Mode:          mode,
		SourceProfile: strings.TrimSpace(entry.SourceProfile),
		BaseURL:       baseURL,
		Token:         strings.TrimSpace(entry.Token),
		TokenEnv:      strings.TrimSpace(entry.TokenEnv),
		Timeout:       timeout,
		ConfigRef:     strings.TrimSpace(entry.ConfigRef),
	}, nil
}

func (e Entry) ResolvedToken() string {
	if token := strings.TrimSpace(e.Token); token != "" {
		return token
	}
	if envKey := strings.TrimSpace(e.TokenEnv); envKey != "" {
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
