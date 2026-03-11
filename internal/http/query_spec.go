package httpserver

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func (s *Server) handleGetSpec(c *fiber.Ctx) error {
	query, err := parseSpecQuery(c)
	if err != nil {
		return s.writeQueryError(c, err)
	}

	resolved, err := s.readStore.ResolveSpecSnapshots(c.Context(), query.Snapshot)
	if err != nil {
		return s.writeQueryError(c, err)
	}

	switch len(resolved.Candidates) {
	case 0:
		return s.writeQueryError(c, fmt.Errorf("%w: repo=%q", errSpecNotFound, query.Snapshot.RepoPath))
	case 1:
	default:
		return writeAPIAmbiguity(c, "spec query is ambiguous across APIs", resolved.Candidates)
	}

	artifact, err := s.readStore.GetSpecArtifactByAPISpecRevisionID(
		c.Context(),
		resolved.Candidates[0].APISpecRevisionID,
	)
	if err != nil {
		return s.writeQueryError(c, err)
	}

	return writeSpecArtifactResponse(c, artifact, query.Format)
}
