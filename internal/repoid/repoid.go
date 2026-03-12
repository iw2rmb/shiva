package repoid

import (
	"fmt"
	"strings"
)

type Identity struct {
	Namespace string
	Repo      string
}

func (i Identity) Path() string {
	if strings.TrimSpace(i.Namespace) == "" || strings.TrimSpace(i.Repo) == "" {
		return ""
	}
	return i.Namespace + "/" + i.Repo
}

func Normalize(namespace, repo string) (Identity, error) {
	normalized := Identity{
		Namespace: strings.TrimSpace(namespace),
		Repo:      strings.TrimSpace(repo),
	}
	switch {
	case normalized.Namespace == "":
		return Identity{}, fmt.Errorf("namespace must not be empty")
	case normalized.Repo == "":
		return Identity{}, fmt.Errorf("repo must not be empty")
	default:
		return normalized, nil
	}
}

func ParsePath(path string) (Identity, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return Identity{}, fmt.Errorf("repo path must not be empty")
	}

	slashIndex := strings.LastIndex(trimmed, "/")
	if slashIndex <= 0 || slashIndex == len(trimmed)-1 {
		return Identity{}, fmt.Errorf("repo path must be <namespace>/<repo>")
	}

	return Normalize(trimmed[:slashIndex], trimmed[slashIndex+1:])
}
