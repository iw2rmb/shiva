package httpserver

import (
	"fmt"
	"strings"
)

type readRoutePath struct {
	APIRoot     string
	Selector    string
	HasSelector bool
	Target      string
}

func parseReadRoutePath(raw string) (readRoutePath, error) {
	path, err := normalizeDelimitedReadPath(raw)
	if err != nil {
		return readRoutePath{}, err
	}

	segments := strings.Split(path, "/")
	if len(segments) == 0 || segments[0] == "" {
		return readRoutePath{}, fmt.Errorf("route path must not be empty")
	}

	result := readRoutePath{}

	if segments[0] == "-" {
		end := -1
		for i := 1; i < len(segments); i++ {
			if segments[i] == "-" {
				end = i
				break
			}
		}
		if end < 0 {
			return readRoutePath{}, fmt.Errorf("invalid monorepo route: missing closing /-/ delimiter")
		}
		if end == 1 {
			return readRoutePath{}, fmt.Errorf("invalid monorepo route: api path is required")
		}

		result.APIRoot = strings.Join(segments[1:end], "/")
		result.APIRoot = strings.TrimSpace(result.APIRoot)
		if result.APIRoot == "" {
			return readRoutePath{}, fmt.Errorf("invalid monorepo route: api path is required")
		}

		segments = segments[end+1:]
		if len(segments) == 0 {
			return readRoutePath{}, fmt.Errorf("invalid monorepo route: target path is required")
		}
	}

	if len(segments) == 0 {
		return readRoutePath{}, fmt.Errorf("route path must not be empty")
	}

	if len(segments) == 1 {
		result.Target = segments[0]
		return result, nil
	}

	result.HasSelector = true
	result.Selector = segments[0]
	result.Target = strings.Join(segments[1:], "/")
	if result.Target == "" {
		return readRoutePath{}, fmt.Errorf("route target must not be empty")
	}
	return result, nil
}

func normalizeDelimitedReadPath(raw string) (string, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return "", fmt.Errorf("route path must not be empty")
	}

	clean = strings.TrimPrefix(clean, "/")
	if clean == "" {
		return "", fmt.Errorf("route path must not be empty")
	}

	return clean, nil
}
