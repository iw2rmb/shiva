package httpserver

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func (s *Server) handleGetOperation(c *fiber.Ctx) error {
	envelope, err := parseOperationEndpointQuery(c)
	if err != nil {
		return s.writeQueryError(c, err)
	}

	resolved, err := s.resolveOperationCandidates(c.Context(), envelope)
	if err != nil {
		return s.writeQueryError(c, err)
	}

	switch len(resolved.Candidates) {
	case 0:
		return s.writeQueryError(c, fmt.Errorf("%w: repo=%q", errOperationNotFound, resolved.Snapshot.Repo.Path()))
	case 1:
		return writeRawOperationResponse(c, resolved.Candidates[0].RawJSON)
	default:
		return writeOperationAmbiguity(c, "operation query is ambiguous", resolved.Candidates)
	}
}
