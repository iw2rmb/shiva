package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/iw2rmb/shiva/internal/cli/profile"
	"github.com/iw2rmb/shiva/internal/cli/target"
	"gopkg.in/yaml.v3"
)

const (
	defaultBaseURL        = "http://127.0.0.1:8080"
	defaultRequestTimeout = 10 * time.Second
	configFileName        = "profiles.yaml"
	configDirName         = "shiva"
)

type EnvOverrides struct {
	BaseURL string
	Timeout time.Duration
}

type Paths struct {
	ConfigHome string
	CacheHome  string
}

type Document struct {
	ActiveProfile string
	Profiles      map[string]profile.Source
	Targets       map[string]target.Entry
	Path          string
}

type LoadOptions struct {
	ConfigHome string
	Overrides  EnvOverrides
}

type rawDocument struct {
	ActiveProfile string               `yaml:"active_profile"`
	Profiles      map[string]rawSource `yaml:"profiles"`
	Targets       map[string]rawTarget `yaml:"targets"`
}

type rawSource struct {
	BaseURL  string        `yaml:"base_url"`
	Token    string        `yaml:"token"`
	TokenEnv string        `yaml:"token_env"`
	Timeout  durationValue `yaml:"timeout"`
}

type rawTarget struct {
	Mode          string        `yaml:"mode"`
	SourceProfile string        `yaml:"source_profile"`
	BaseURL       string        `yaml:"base_url"`
	Token         string        `yaml:"token"`
	TokenEnv      string        `yaml:"token_env"`
	Timeout       durationValue `yaml:"timeout"`
}

type durationValue struct {
	set      bool
	duration time.Duration
}

func ResolvePaths() (Paths, error) {
	configHome, err := resolveXDGPath("XDG_CONFIG_HOME", ".config")
	if err != nil {
		return Paths{}, fmt.Errorf("resolve XDG config dir: %w", err)
	}
	cacheHome, err := resolveXDGPath("XDG_CACHE_HOME", ".cache")
	if err != nil {
		return Paths{}, fmt.Errorf("resolve XDG cache dir: %w", err)
	}
	return Paths{
		ConfigHome: configHome,
		CacheHome:  cacheHome,
	}, nil
}

func LoadEnvOverrides() (EnvOverrides, error) {
	timeout := defaultRequestTimeout
	if rawTimeout, ok := os.LookupEnv("SHIVA_REQUEST_TIMEOUT_SECONDS"); ok {
		timeoutSeconds, err := strconv.Atoi(strings.TrimSpace(rawTimeout))
		if err != nil {
			return EnvOverrides{}, fmt.Errorf("invalid SHIVA_REQUEST_TIMEOUT_SECONDS: %w", err)
		}
		if timeoutSeconds < 1 {
			return EnvOverrides{}, errors.New("SHIVA_REQUEST_TIMEOUT_SECONDS must be at least 1")
		}
		timeout = time.Duration(timeoutSeconds) * time.Second
	}

	baseURL := defaultBaseURL
	if rawBaseURL, ok := os.LookupEnv("SHIVA_BASE_URL"); ok && strings.TrimSpace(rawBaseURL) != "" {
		baseURL = strings.TrimSpace(rawBaseURL)
	}
	return EnvOverrides{
		BaseURL: baseURL,
		Timeout: timeout,
	}, nil
}

func LoadDocument(options LoadOptions) (Document, error) {
	configHome := strings.TrimSpace(options.ConfigHome)
	if configHome == "" {
		paths, err := ResolvePaths()
		if err != nil {
			return Document{}, err
		}
		configHome = paths.ConfigHome
	}

	configPath := filepath.Join(configHome, configDirName, configFileName)
	content, err := os.ReadFile(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			overrides := options.Overrides
			if overrides == (EnvOverrides{}) {
				loadedOverrides, loadErr := LoadEnvOverrides()
				if loadErr != nil {
					return Document{}, loadErr
				}
				overrides = loadedOverrides
			}
			return defaultDocument(configPath, overrides)
		}
		return Document{}, fmt.Errorf("read CLI config %s: %w", configPath, err)
	}

	var raw rawDocument
	if err := yaml.Unmarshal(content, &raw); err != nil {
		return Document{}, fmt.Errorf("decode CLI config %s: %w", configPath, err)
	}

	document := Document{
		ActiveProfile: strings.TrimSpace(raw.ActiveProfile),
		Profiles:      make(map[string]profile.Source, len(raw.Profiles)),
		Targets:       make(map[string]target.Entry, len(raw.Targets)+1),
		Path:          configPath,
	}

	for name, rawProfile := range raw.Profiles {
		normalized, err := profile.Normalize(name, profile.Source{
			BaseURL:   rawProfile.BaseURL,
			Token:     rawProfile.Token,
			TokenEnv:  rawProfile.TokenEnv,
			Timeout:   rawProfile.Timeout.value(defaultRequestTimeout),
			ConfigRef: configPath,
		})
		if err != nil {
			return Document{}, err
		}
		document.Profiles[normalized.Name] = normalized
	}

	for name, rawTarget := range raw.Targets {
		normalized, err := target.Normalize(name, target.Entry{
			Mode:          target.Mode(rawTarget.Mode),
			SourceProfile: rawTarget.SourceProfile,
			BaseURL:       rawTarget.BaseURL,
			Token:         rawTarget.Token,
			TokenEnv:      rawTarget.TokenEnv,
			Timeout:       rawTarget.Timeout.value(defaultRequestTimeout),
			ConfigRef:     configPath,
		})
		if err != nil {
			return Document{}, err
		}
		document.Targets[normalized.Name] = normalized
	}

	if _, ok := document.Targets[target.BuiltinShivaName]; !ok {
		document.Targets[target.BuiltinShivaName] = target.BuiltinShiva()
	}

	if len(document.Profiles) == 0 {
		return Document{}, fmt.Errorf("CLI config %s must define at least one profile", configPath)
	}

	if document.ActiveProfile == "" {
		if len(document.Profiles) == 1 {
			for name := range document.Profiles {
				document.ActiveProfile = name
			}
		} else if _, ok := document.Profiles[profile.DefaultName]; ok {
			document.ActiveProfile = profile.DefaultName
		} else {
			return Document{}, fmt.Errorf("CLI config %s must define active_profile", configPath)
		}
	}

	if _, ok := document.Profiles[document.ActiveProfile]; !ok {
		return Document{}, fmt.Errorf("active_profile %q is not defined in %s", document.ActiveProfile, configPath)
	}

	for _, entry := range document.Targets {
		if entry.SourceProfile == "" {
			continue
		}
		if _, ok := document.Profiles[entry.SourceProfile]; !ok {
			return Document{}, fmt.Errorf(
				"target %q references unknown source_profile %q in %s",
				entry.Name,
				entry.SourceProfile,
				configPath,
			)
		}
	}

	return document, nil
}

func (d Document) ResolveSource(requestedProfile string, requestedTarget string) (profile.Source, *target.Entry, error) {
	if len(d.Profiles) == 0 {
		return profile.Source{}, nil, errors.New("CLI source profiles are not configured")
	}

	profileName := strings.TrimSpace(d.ActiveProfile)
	if profileName == "" {
		profileName = profile.DefaultName
	}

	var resolvedTarget *target.Entry
	if targetName := strings.TrimSpace(requestedTarget); targetName != "" {
		entry, ok := d.Targets[targetName]
		if !ok {
			return profile.Source{}, nil, fmt.Errorf("target %q is not configured", targetName)
		}
		resolvedTarget = &entry
		if entry.SourceProfile != "" {
			profileName = entry.SourceProfile
		}
	}

	if explicitProfile := strings.TrimSpace(requestedProfile); explicitProfile != "" {
		profileName = explicitProfile
	}

	resolvedProfile, ok := d.Profiles[profileName]
	if !ok {
		return profile.Source{}, nil, fmt.Errorf("profile %q is not configured", profileName)
	}
	return resolvedProfile, resolvedTarget, nil
}

func defaultDocument(configPath string, overrides EnvOverrides) (Document, error) {
	defaultProfile, err := profile.Normalize(profile.DefaultName, profile.Source{
		BaseURL:   firstNonEmpty(overrides.BaseURL, defaultBaseURL),
		Timeout:   firstPositiveDuration(overrides.Timeout, defaultRequestTimeout),
		ConfigRef: configPath,
	})
	if err != nil {
		return Document{}, err
	}

	return Document{
		ActiveProfile: defaultProfile.Name,
		Profiles: map[string]profile.Source{
			defaultProfile.Name: defaultProfile,
		},
		Targets: map[string]target.Entry{
			target.BuiltinShivaName: target.BuiltinShiva(),
		},
		Path: configPath,
	}, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstPositiveDuration(values ...time.Duration) time.Duration {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func resolveXDGPath(envKey string, fallbackDir string) (string, error) {
	if rawValue, ok := os.LookupEnv(envKey); ok && strings.TrimSpace(rawValue) != "" {
		value := strings.TrimSpace(rawValue)
		if !filepath.IsAbs(value) {
			return "", fmt.Errorf("%s must be an absolute path", envKey)
		}
		return value, nil
	}

	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		return "", errors.New("HOME must be set")
	}
	if !filepath.IsAbs(home) {
		return "", errors.New("HOME must be an absolute path")
	}
	return filepath.Join(home, fallbackDir), nil
}

func (d *durationValue) UnmarshalYAML(node *yaml.Node) error {
	if node == nil {
		return nil
	}
	d.set = true

	switch node.Kind {
	case yaml.ScalarNode:
		switch node.ShortTag() {
		case "!!int":
			seconds, err := strconv.Atoi(strings.TrimSpace(node.Value))
			if err != nil {
				return fmt.Errorf("invalid timeout seconds %q: %w", node.Value, err)
			}
			if seconds < 1 {
				return fmt.Errorf("timeout must be at least 1 second")
			}
			d.duration = time.Duration(seconds) * time.Second
			return nil
		default:
			value := strings.TrimSpace(node.Value)
			if value == "" {
				d.duration = 0
				return nil
			}
			if seconds, err := strconv.Atoi(value); err == nil {
				if seconds < 1 {
					return fmt.Errorf("timeout must be at least 1 second")
				}
				d.duration = time.Duration(seconds) * time.Second
				return nil
			}
			duration, err := time.ParseDuration(value)
			if err != nil {
				return fmt.Errorf("invalid timeout duration %q: %w", node.Value, err)
			}
			if duration <= 0 {
				return fmt.Errorf("timeout must be greater than zero")
			}
			d.duration = duration
			return nil
		}
	default:
		return fmt.Errorf("timeout must be a scalar value")
	}
}

func (d durationValue) value(fallback time.Duration) time.Duration {
	if !d.set || d.duration <= 0 {
		return fallback
	}
	return d.duration
}
