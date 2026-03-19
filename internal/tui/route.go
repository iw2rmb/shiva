package tui

import (
	"fmt"
	"strings"
)

type RouteKind string

const (
	RouteHome         RouteKind = "home"
	RouteNamespaces   RouteKind = "namespaces"
	RouteRepos        RouteKind = "repos"
	RouteRepoExplorer RouteKind = "repo_explorer"
)

type InitialRoute struct {
	Kind      RouteKind
	Namespace string
	Repo      string
}

func (route InitialRoute) Validate() error {
	switch route.Kind {
	case RouteHome:
		if route.Namespace != "" || route.Repo != "" {
			return fmt.Errorf("namespace and repo must be empty for %q route", route.Kind)
		}
	case RouteNamespaces:
		if route.Namespace != "" || route.Repo != "" {
			return fmt.Errorf("namespace and repo must be empty for %q route", route.Kind)
		}
	case RouteRepos:
		if strings.TrimSpace(route.Namespace) == "" {
			return fmt.Errorf("namespace must not be empty for %q route", route.Kind)
		}
		if strings.HasSuffix(route.Namespace, "/") {
			return fmt.Errorf("namespace must not end with / for %q route", route.Kind)
		}
		if route.Repo != "" {
			return fmt.Errorf("repo must be empty for %q route", route.Kind)
		}
	case RouteRepoExplorer:
		if strings.TrimSpace(route.Namespace) == "" || strings.TrimSpace(route.Repo) == "" {
			return fmt.Errorf("namespace and repo must not be empty for %q route", route.Kind)
		}
		if strings.HasSuffix(route.Namespace, "/") || strings.Contains(route.Repo, "/") {
			return fmt.Errorf("repo route must be normalized for %q route", route.Kind)
		}
	default:
		return fmt.Errorf("unsupported route %q", route.Kind)
	}

	return nil
}
