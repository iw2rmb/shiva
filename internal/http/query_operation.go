package httpserver

import (
	"fmt"

	"github.com/gofiber/fiber/v2"

	"github.com/iw2rmb/shiva/internal/store"
)

func (s *Server) handleGetOperation(c *fiber.Ctx) error {
	snapshot, selector, err := parseOperationEndpointQuery(c)
	if err != nil {
		return s.writeQueryError(c, err)
	}

	var resolved store.ResolvedOperationCandidates
	if selector.OperationID != "" {
		resolved, err = s.readStore.ResolveOperationCandidatesByOperationID(
			c.Context(),
			store.ResolveOperationByIDInput{
				ResolveReadSnapshotInput: snapshot,
				OperationID:              selector.OperationID,
			},
		)
	} else {
		resolved, err = s.readStore.ResolveOperationCandidatesByMethodPath(
			c.Context(),
			store.ResolveOperationByMethodPathInput{
				ResolveReadSnapshotInput: snapshot,
				Method:                   selector.Method,
				Path:                     selector.Path,
			},
		)
	}
	if err != nil {
		return s.writeQueryError(c, err)
	}

	switch len(resolved.Candidates) {
	case 0:
		return s.writeQueryError(c, fmt.Errorf("%w: repo=%q", errOperationNotFound, resolved.Snapshot.Repo.PathWithNamespace))
	case 1:
		return writeRawOperationResponse(c, resolved.Candidates[0].RawJSON)
	default:
		return writeOperationAmbiguity(c, "operation query is ambiguous", resolved.Candidates)
	}
}
