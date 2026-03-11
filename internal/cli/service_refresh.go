package cli

import (
	"strings"

	"github.com/iw2rmb/shiva/internal/cli/catalog"
	"github.com/iw2rmb/shiva/internal/cli/request"
)

func (s *RuntimeService) refreshOptions(
	category string,
	profileName string,
	selector request.Envelope,
	options RequestOptions,
) catalog.RefreshOptions {
	refresh := options.Refresh
	if refresh {
		refresh = s.markRefresh(category, profileName, selector)
	}
	return catalog.RefreshOptions{
		Refresh: refresh,
		Offline: options.Offline,
	}
}

func (s *RuntimeService) markRefresh(category string, profileName string, selector request.Envelope) bool {
	if s == nil {
		return true
	}

	scope := catalog.ScopeFromSelector(selector.RevisionID, selector.SHA)
	key := strings.Join([]string{
		category,
		strings.TrimSpace(profileName),
		strings.TrimSpace(selector.Repo),
		strings.TrimSpace(selector.API),
		scope.Key,
	}, "\x00")

	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()

	if _, ok := s.refreshedKeys[key]; ok {
		return false
	}
	s.refreshedKeys[key] = struct{}{}
	return true
}
