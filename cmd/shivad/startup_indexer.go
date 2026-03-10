package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/iw2rmb/shiva/internal/config"
	"github.com/iw2rmb/shiva/internal/gitlab"
	"github.com/iw2rmb/shiva/internal/store"
)

const startupIndexingEventType = "startup.index"

type startupIndexingStore interface {
	CountRevisions(ctx context.Context) (int64, error)
	PersistGitLabWebhook(ctx context.Context, input store.GitLabIngestInput) (store.GitLabIngestResult, error)
}

type startupIndexingGitLabClient interface {
	VisitProjects(ctx context.Context, visit func(gitlab.Project) error) (int, error)
	GetBranch(ctx context.Context, projectID int64, branch string) (gitlab.Branch, error)
}

type startupIndexingPayload struct {
	Source            string `json:"source"`
	GitLabProjectID   int64  `json:"gitlab_project_id"`
	PathWithNamespace string `json:"path_with_namespace"`
	DefaultBranch     string `json:"default_branch"`
	Sha               string `json:"sha"`
}

func enqueueStartupIndexingIfEmpty(
	ctx context.Context,
	cfg config.Config,
	logger *slog.Logger,
	storeInstance startupIndexingStore,
	gitlabClient startupIndexingGitLabClient,
) error {
	if storeInstance == nil {
		return errors.New("startup indexing store is not configured")
	}
	if gitlabClient == nil {
		return errors.New("startup indexing gitlab client is not configured")
	}

	revisionCount, err := storeInstance.CountRevisions(ctx)
	if err != nil {
		return fmt.Errorf("count revisions before startup indexing: %w", err)
	}
	if revisionCount > 0 {
		return nil
	}

	if logger != nil {
		logger.Info("startup indexing started", "reason", "no revisions recorded")
	}

	var (
		enqueued                int
		duplicates              int
		skippedInvalidProject   int
		skippedPersonalProject  int
		skippedNoDefaultBranch  int
		skippedMissingBranchSHA int
	)

	projectCount, err := gitlabClient.VisitProjects(ctx, func(project gitlab.Project) error {
		projectID := project.ID
		pathWithNamespace := strings.TrimSpace(project.PathWithNamespace)
		defaultBranch := strings.TrimSpace(project.DefaultBranch)
		namespaceKind := strings.TrimSpace(project.NamespaceKind)

		switch {
		case projectID < 1:
			skippedInvalidProject++
			if logger != nil {
				logger.Warn("startup indexing skipped project with invalid id")
			}
			return nil
		case pathWithNamespace == "":
			skippedInvalidProject++
			if logger != nil {
				logger.Warn("startup indexing skipped project with empty path", "project_id", projectID)
			}
			return nil
		case namespaceKind == "user":
			skippedPersonalProject++
			if logger != nil {
				logger.Info(
					"startup indexing skipped personal project",
					"project_id", projectID,
					"path_with_namespace", pathWithNamespace,
				)
			}
			return nil
		case defaultBranch == "":
			skippedNoDefaultBranch++
			if logger != nil {
				logger.Info(
					"startup indexing skipped project without default branch",
					"project_id", projectID,
					"path_with_namespace", pathWithNamespace,
				)
			}
			return nil
		}

		branch, err := gitlabClient.GetBranch(ctx, projectID, defaultBranch)
		if err != nil {
			if errors.Is(err, gitlab.ErrNotFound) {
				skippedMissingBranchSHA++
				if logger != nil {
					logger.Warn(
						"startup indexing skipped project with missing default branch",
						"project_id", projectID,
						"path_with_namespace", pathWithNamespace,
						"default_branch", defaultBranch,
					)
				}
				return nil
			}
			return fmt.Errorf(
				"resolve startup indexing branch head for project %d (%s): %w",
				projectID,
				pathWithNamespace,
				err,
			)
		}

		sha := strings.TrimSpace(branch.CommitID)
		if sha == "" {
			skippedMissingBranchSHA++
			if logger != nil {
				logger.Warn(
					"startup indexing skipped project with empty default branch head",
					"project_id", projectID,
					"path_with_namespace", pathWithNamespace,
					"default_branch", defaultBranch,
				)
			}
			return nil
		}

		payloadJSON, err := json.Marshal(startupIndexingPayload{
			Source:            "startup_indexer",
			GitLabProjectID:   projectID,
			PathWithNamespace: pathWithNamespace,
			DefaultBranch:     defaultBranch,
			Sha:               sha,
		})
		if err != nil {
			return fmt.Errorf("marshal startup indexing payload for project %d (%s): %w", projectID, pathWithNamespace, err)
		}

		result, err := storeInstance.PersistGitLabWebhook(ctx, store.GitLabIngestInput{
			TenantKey:         cfg.TenantKey,
			GitLabProjectID:   projectID,
			PathWithNamespace: pathWithNamespace,
			DefaultBranch:     defaultBranch,
			Sha:               sha,
			Branch:            defaultBranch,
			ParentSha:         "",
			EventType:         startupIndexingEventType,
			DeliveryID:        startupIndexingDeliveryID(projectID, sha),
			PayloadJSON:       payloadJSON,
		})
		if err != nil {
			return fmt.Errorf("enqueue startup indexing event for project %d (%s): %w", projectID, pathWithNamespace, err)
		}
		if result.Duplicate {
			duplicates++
			return nil
		}
		enqueued++
		return nil
	})
	if err != nil {
		return fmt.Errorf("list gitlab projects for startup indexing: %w", err)
	}

	if logger != nil {
		logger.Info(
			"startup indexing finished",
			"project_count", projectCount,
			"enqueued", enqueued,
			"duplicates", duplicates,
			"skipped_invalid_project", skippedInvalidProject,
			"skipped_personal_project", skippedPersonalProject,
			"skipped_no_default_branch", skippedNoDefaultBranch,
			"skipped_missing_branch_sha", skippedMissingBranchSHA,
		)
	}

	return nil
}

func startupIndexingDeliveryID(projectID int64, sha string) string {
	return fmt.Sprintf("startup-index:%d:%s", projectID, strings.TrimSpace(sha))
}
