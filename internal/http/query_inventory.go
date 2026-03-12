package httpserver

import (
	"fmt"

	"github.com/gofiber/fiber/v2"

	"github.com/iw2rmb/shiva/internal/repoid"
)

func (s *Server) handleListAPIs(c *fiber.Ctx) error {
	snapshotQuery, err := parseAPIsQuery(c)
	if err != nil {
		return s.writeQueryError(c, err)
	}

	resolved, err := s.readStore.ResolveReadSnapshot(c.Context(), snapshotQuery)
	if err != nil {
		return s.writeQueryError(c, err)
	}

	items, err := s.readStore.ListAPISnapshotInventoryByRepoRevision(
		c.Context(),
		resolved.Repo.ID,
		resolved.Revision.ID,
	)
	if err != nil {
		return s.writeQueryError(c, err)
	}

	return c.Status(fiber.StatusOK).JSON(mapAPISnapshots(items))
}

func (s *Server) handleListOperations(c *fiber.Ctx) error {
	snapshotQuery, err := parseOperationsQuery(c)
	if err != nil {
		return s.writeQueryError(c, err)
	}

	resolved, err := s.readStore.ResolveReadSnapshot(c.Context(), snapshotQuery)
	if err != nil {
		return s.writeQueryError(c, err)
	}

	var itemsResponse []operationSnapshotResponse
	if snapshotQuery.APIPath != "" {
		apiSnapshot, found, err := s.readStore.GetAPISnapshotByRepoRevisionAndAPI(
			c.Context(),
			resolved.Repo.ID,
			snapshotQuery.APIPath,
			resolved.Revision.ID,
		)
		if err != nil {
			return s.writeQueryError(c, err)
		}
		if !found || !apiSnapshot.HasSnapshot {
			return s.writeQueryError(
				c,
				fmt.Errorf("%w: repo=%q api=%q", errAPISnapshotNotFound, resolved.Repo.Path(), snapshotQuery.APIPath),
			)
		}

		items, err := s.readStore.ListOperationInventoryByRepoRevisionAndAPI(
			c.Context(),
			resolved.Repo.ID,
			snapshotQuery.APIPath,
			resolved.Revision.ID,
		)
		if err != nil {
			return s.writeQueryError(c, err)
		}

		itemsResponse, err = mapOperationSnapshots(items, true)
		if err != nil {
			return s.writeQueryError(c, err)
		}
	} else {
		items, err := s.readStore.ListOperationInventoryByRepoRevision(
			c.Context(),
			resolved.Repo.ID,
			resolved.Revision.ID,
		)
		if err != nil {
			return s.writeQueryError(c, err)
		}

		itemsResponse, err = mapOperationSnapshots(items, true)
		if err != nil {
			return s.writeQueryError(c, err)
		}
	}

	return c.Status(fiber.StatusOK).JSON(itemsResponse)
}

func (s *Server) handleListRepos(c *fiber.Ctx) error {
	if err := parseReposQuery(c); err != nil {
		return s.writeQueryError(c, err)
	}

	items, err := s.readStore.ListRepoCatalogInventory(c.Context())
	if err != nil {
		return s.writeQueryError(c, err)
	}

	return c.Status(fiber.StatusOK).JSON(mapRepoCatalogEntries(items))
}

func (s *Server) handleGetCatalogStatus(c *fiber.Ctx) error {
	repoPath, err := parseCatalogStatusQuery(c)
	if err != nil {
		return s.writeQueryError(c, err)
	}

	identity, err := repoid.ParsePath(repoPath)
	if err != nil {
		return s.writeQueryError(c, err)
	}

	item, err := s.readStore.GetRepoCatalogFreshness(c.Context(), identity.Namespace, identity.Repo)
	if err != nil {
		return s.writeQueryError(c, err)
	}

	return c.Status(fiber.StatusOK).JSON(mapRepoCatalogEntry(item))
}
