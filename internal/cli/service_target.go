package cli

import (
	"github.com/iw2rmb/shiva/internal/cli/profile"
	"github.com/iw2rmb/shiva/internal/cli/target"
)

func (s *RuntimeService) resolveSourceAndTarget(
	requestedProfile string,
	requestedTarget string,
) (profile.Source, *target.Entry, error) {
	resolvedProfile, resolvedTarget, err := s.document.ResolveSource(requestedProfile, requestedTarget)
	if err != nil {
		return profile.Source{}, nil, &InvalidInputError{Message: err.Error()}
	}
	return resolvedProfile, resolvedTarget, nil
}
