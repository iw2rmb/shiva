package httpserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/iw2rmb/shiva/internal/store"
)

type gitlabWebhookIngestor interface {
	PersistGitLabWebhook(ctx context.Context, input store.GitLabIngestInput) (store.GitLabIngestResult, error)
}

type gitLabWebhookPayload struct {
	ObjectKind string `json:"object_kind"`
	Project    struct {
		ID                int64  `json:"id"`
		PathWithNamespace string `json:"path_with_namespace"`
		DefaultBranch     string `json:"default_branch"`
	} `json:"project"`
}

type gitlabWebhookResponse struct {
	Status    string `json:"status"`
	EventID   int64  `json:"event_id"`
	Duplicate bool   `json:"duplicate"`
}

func (s *Server) handleGitLabWebhook(c *fiber.Ctx) error {
	if strings.TrimSpace(s.cfg.GitLabWebhookSecret) == "" {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "gitlab webhook secret is not configured",
		})
	}

	headerToken := strings.TrimSpace(c.Get("X-Gitlab-Token"))
	if headerToken == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "missing X-Gitlab-Token header",
		})
	}
	if !secureEqual(headerToken, s.cfg.GitLabWebhookSecret) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "invalid X-Gitlab-Token",
		})
	}

	body := c.Body()
	if len(body) == 0 || !json.Valid(body) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "request body must be valid JSON",
		})
	}

	var payload gitLabWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "failed to parse GitLab webhook payload",
		})
	}

	deliveryID := extractDeliveryID(c)
	if deliveryID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "missing GitLab delivery id header",
		})
	}

	eventType := strings.TrimSpace(c.Get("X-Gitlab-Event"))
	if eventType == "" {
		eventType = strings.TrimSpace(payload.ObjectKind)
	}

	result, err := s.gitlabIngestor.PersistGitLabWebhook(c.Context(), store.GitLabIngestInput{
		TenantKey:         s.cfg.TenantKey,
		GitLabProjectID:   payload.Project.ID,
		PathWithNamespace: payload.Project.PathWithNamespace,
		DefaultBranch:     payload.Project.DefaultBranch,
		EventType:         eventType,
		DeliveryID:        deliveryID,
		PayloadJSON:       body,
	})
	if err != nil {
		switch {
		case errors.Is(err, store.ErrStoreNotConfigured):
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
				"error": "database is not configured",
			})
		case errors.Is(err, store.ErrInvalidIngestInput):
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": err.Error(),
			})
		default:
			s.logger.Error("failed to persist GitLab ingest event", "error", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to persist webhook event",
			})
		}
	}

	statusCode := fiber.StatusAccepted
	if result.Duplicate {
		statusCode = fiber.StatusOK
	}

	return c.Status(statusCode).JSON(gitlabWebhookResponse{
		Status:    "accepted",
		EventID:   result.EventID,
		Duplicate: result.Duplicate,
	})
}

func extractDeliveryID(c *fiber.Ctx) string {
	candidates := []string{
		"X-Gitlab-Delivery",
		"X-Gitlab-Event-UUID",
		"X-Gitlab-Webhook-UUID",
	}
	for _, name := range candidates {
		if value := strings.TrimSpace(c.Get(name)); value != "" {
			return value
		}
	}
	return ""
}

func secureEqual(provided, expected string) bool {
	if len(provided) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}
