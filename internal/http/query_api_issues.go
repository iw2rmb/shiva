package httpserver

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func (s *Server) handleGetAPIIssues(c *fiber.Ctx) error {
	query, err := parseAPIIssuesQuery(c)
	if err != nil {
		return s.writeQueryError(c, err)
	}

	resolved, err := s.readStore.ResolveReadSnapshot(c.Context(), query.Snapshot)
	if err != nil {
		return s.writeQueryError(c, err)
	}

	apiSnapshot, found, err := s.readStore.GetAPISnapshotByRepoRevisionAndAPI(
		c.Context(),
		resolved.Repo.ID,
		query.Snapshot.APIPath,
		resolved.Revision.ID,
	)
	if err != nil {
		return s.writeQueryError(c, err)
	}
	if !found || !apiSnapshot.HasSnapshot {
		return s.writeQueryError(
			c,
			fmt.Errorf("%w: repo=%q api=%q", errAPISnapshotNotFound, resolved.Repo.Path(), query.Snapshot.APIPath),
		)
	}

	revision, err := s.readStore.GetAPISpecRevisionByID(c.Context(), apiSnapshot.APISpecRevisionID)
	if err != nil {
		return s.writeQueryError(c, err)
	}

	issues, err := s.readStore.ListVacuumIssuesByAPISpecRevisionID(c.Context(), apiSnapshot.APISpecRevisionID)
	if err != nil {
		return s.writeQueryError(c, err)
	}

	return c.Status(fiber.StatusOK).JSON(apiIssuesResponse{
		Namespace:         resolved.Repo.Namespace,
		Repo:              resolved.Repo.Repo,
		API:               query.Snapshot.APIPath,
		APISpecRevisionID: apiSnapshot.APISpecRevisionID,
		VacuumStatus:      revision.VacuumStatus,
		VacuumError:       revision.VacuumError,
		VacuumValidatedAt: revision.VacuumValidatedAt,
		Issues:            mapVacuumIssues(issues),
	})
}
