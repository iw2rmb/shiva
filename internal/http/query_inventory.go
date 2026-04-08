package httpserver

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"

	"github.com/iw2rmb/shiva/internal/repoid"
	"github.com/iw2rmb/shiva/internal/store"
)

type catalogCountResponse struct {
	TotalCount    int64 `json:"total_count"`
	MaxItemLength int64 `json:"max_item_length"`
}

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
	query, err := parseOperationsQuery(c)
	if err != nil {
		return s.writeQueryError(c, err)
	}
	snapshotQuery := query.Snapshot

	if snapshotQuery.Repo == "" {
		items, listErr := s.readStore.ListOperationCatalogInventoryPage(
			c.Context(),
			snapshotQuery.Namespace,
			"",
			query.Query,
			query.Limit,
			query.Offset,
		)
		if listErr != nil {
			return s.writeQueryError(c, listErr)
		}

		itemsResponse, mapErr := mapOperationSnapshots(items, true)
		if mapErr != nil {
			return s.writeQueryError(c, mapErr)
		}
		return c.Status(fiber.StatusOK).JSON(itemsResponse)
	}

	resolved, err := s.readStore.ResolveReadSnapshot(c.Context(), snapshotQuery)
	if err != nil {
		return s.writeQueryError(c, err)
	}

	var items []store.OperationSnapshot
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

		items, err = s.readStore.ListOperationInventoryByRepoRevisionAndAPIPage(
			c.Context(),
			resolved.Repo.ID,
			snapshotQuery.APIPath,
			resolved.Revision.ID,
			query.Query,
			query.Limit,
			query.Offset,
		)
		if err != nil {
			return s.writeQueryError(c, err)
		}

	} else {
		items, err = s.readStore.ListOperationInventoryByRepoRevisionPage(
			c.Context(),
			resolved.Repo.ID,
			resolved.Revision.ID,
			query.Query,
			query.Limit,
			query.Offset,
		)
		if err != nil {
			return s.writeQueryError(c, err)
		}

	}
	items = withOperationRepoIdentity(items, resolved.Repo.Namespace, resolved.Repo.Repo)
	itemsResponse, err := mapOperationSnapshots(items, true)
	if err != nil {
		return s.writeQueryError(c, err)
	}

	return c.Status(fiber.StatusOK).JSON(itemsResponse)
}

func withOperationRepoIdentity(items []store.OperationSnapshot, namespace string, repo string) []store.OperationSnapshot {
	if len(items) == 0 {
		return items
	}
	enriched := make([]store.OperationSnapshot, len(items))
	copy(enriched, items)
	for index := range enriched {
		enriched[index].Namespace = namespace
		enriched[index].Repo = repo
	}
	return enriched
}

func (s *Server) handleListRepos(c *fiber.Ctx) error {
	query, err := parseReposQuery(c)
	if err != nil {
		return s.writeQueryError(c, err)
	}

	items, err := s.readStore.ListRepoCatalogInventory(c.Context())
	if err != nil {
		return s.writeQueryError(c, err)
	}
	filtered := make([]store.RepoCatalogEntry, 0, len(items))
	for _, item := range items {
		if query.Namespace != "" && item.Repo.Namespace != query.Namespace {
			continue
		}
		if !matchesRepoQuery(item, query.Query) {
			continue
		}
		filtered = append(filtered, item)
	}
	filtered = pagedSlice(filtered, query.Offset, query.Limit)

	return c.Status(fiber.StatusOK).JSON(mapRepoCatalogEntries(filtered))
}

func (s *Server) handleListNamespaces(c *fiber.Ctx) error {
	query, err := parseNamespacesQuery(c)
	if err != nil {
		return s.writeQueryError(c, err)
	}

	result, err := s.readStore.ListNamespaceCatalogInventory(c.Context(), store.NamespaceCatalogListInput{
		QueryPrefix: query.QueryPrefix,
		Limit:       query.Limit,
		Offset:      query.Offset,
	})
	if err != nil {
		return s.writeQueryError(c, err)
	}

	c.Set("X-Total-Count", fmt.Sprintf("%d", result.TotalCount))
	c.Set("X-Limit", fmt.Sprintf("%d", query.Limit))
	c.Set("X-Offset", fmt.Sprintf("%d", query.Offset))
	return c.Status(fiber.StatusOK).JSON(mapNamespaceCatalogEntries(result.Items))
}

func (s *Server) handleCountNamespaces(c *fiber.Ctx) error {
	query, err := parseNamespacesCountQuery(c)
	if err != nil {
		return s.writeQueryError(c, err)
	}

	repos, err := s.readStore.ListRepoCatalogInventory(c.Context())
	if err != nil {
		return s.writeQueryError(c, err)
	}

	seen := make(map[string]struct{}, len(repos))
	totalCount := int64(0)
	maxItemLength := int64(0)
	prefix := strings.ToLower(strings.TrimSpace(query.QueryPrefix))
	for _, repo := range repos {
		namespace := strings.TrimSpace(repo.Repo.Namespace)
		if namespace == "" {
			continue
		}
		if prefix != "" && !strings.HasPrefix(strings.ToLower(namespace), prefix) {
			continue
		}
		if _, ok := seen[namespace]; ok {
			continue
		}
		seen[namespace] = struct{}{}
		totalCount++
		length := int64(utf8.RuneCountInString(namespace))
		if length > maxItemLength {
			maxItemLength = length
		}
	}

	return c.Status(fiber.StatusOK).JSON(catalogCountResponse{
		TotalCount:    totalCount,
		MaxItemLength: maxItemLength,
	})
}

func (s *Server) handleCountRepos(c *fiber.Ctx) error {
	query, err := parseReposCountQuery(c)
	if err != nil {
		return s.writeQueryError(c, err)
	}

	repos, err := s.readStore.ListRepoCatalogInventory(c.Context())
	if err != nil {
		return s.writeQueryError(c, err)
	}

	totalCount := int64(0)
	maxItemLength := int64(0)
	for _, repo := range repos {
		if query.Namespace != "" && repo.Repo.Namespace != query.Namespace {
			continue
		}
		if !matchesRepoQuery(repo, query.Query) {
			continue
		}
		totalCount++
		length := int64(utf8.RuneCountInString(strings.TrimSpace(repo.Repo.Repo)))
		if length > maxItemLength {
			maxItemLength = length
		}
	}

	return c.Status(fiber.StatusOK).JSON(catalogCountResponse{
		TotalCount:    totalCount,
		MaxItemLength: maxItemLength,
	})
}

func (s *Server) handleCountOperations(c *fiber.Ctx) error {
	query, err := parseOperationsCountQuery(c)
	if err != nil {
		return s.writeQueryError(c, err)
	}
	queryPrefix := strings.TrimSpace(query.Query)
	if query.Repo != "" {
		resolved, resolveErr := s.readStore.ResolveReadSnapshot(c.Context(), store.ResolveReadSnapshotInput{
			Namespace: query.Namespace,
			Repo:      query.Repo,
		})
		if resolveErr != nil {
			return s.writeQueryError(c, resolveErr)
		}
		count, countErr := s.readStore.CountOperationInventoryByRepoRevision(
			c.Context(),
			resolved.Repo.ID,
			resolved.Revision.ID,
			queryPrefix,
		)
		if countErr != nil {
			return s.writeQueryError(c, countErr)
		}
		return c.Status(fiber.StatusOK).JSON(catalogCountResponse{
			TotalCount:    count.TotalCount,
			MaxItemLength: count.MaxItemLength,
		})
	}

	count, countErr := s.readStore.CountOperationCatalogInventory(
		c.Context(),
		query.Namespace,
		"",
		queryPrefix,
	)
	if countErr != nil {
		return s.writeQueryError(c, countErr)
	}
	return c.Status(fiber.StatusOK).JSON(catalogCountResponse{
		TotalCount:    count.TotalCount,
		MaxItemLength: count.MaxItemLength,
	})
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

func pagedSlice[T any](items []T, offset int32, limit int32) []T {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 && offset == 0 {
		return items
	}
	if offset >= int32(len(items)) {
		return []T{}
	}
	start := int(offset)
	if limit <= 0 {
		return items[start:]
	}
	end := start + int(limit)
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}

func matchesRepoQuery(item store.RepoCatalogEntry, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(item.Repo.Repo)), query)
}

func filterOperationsByQuery(items []store.OperationSnapshot, query string) []store.OperationSnapshot {
	if strings.TrimSpace(query) == "" {
		return items
	}
	filtered := make([]store.OperationSnapshot, 0, len(items))
	for _, item := range items {
		if matchesOperationQuery(item, query) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func matchesOperationQuery(item store.OperationSnapshot, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	label := strings.ToLower(strings.TrimSpace(strings.ToUpper(strings.TrimSpace(item.Method)) + " " + strings.TrimSpace(item.Path)))
	operationID := strings.ToLower(strings.TrimSpace(item.OperationID))
	return strings.HasPrefix(label, query) || strings.HasPrefix(operationID, query)
}
