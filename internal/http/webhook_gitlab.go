package httpserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/iw2rmb/shiva/internal/store"
	"github.com/iw2rmb/shiva/internal/textutil"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type gitlabWebhookIngestor interface {
	PersistGitLabWebhook(ctx context.Context, input store.GitLabIngestInput) (store.GitLabIngestResult, error)
}

type gitLabWebhookPayload struct {
	ObjectKind string `json:"object_kind"`
	Ref        string `json:"ref"`
	Before     string `json:"before"`
	After      string `json:"after"`
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

func (s *Server) handleGitLabWebhook(c *fiber.Ctx) (handlerErr error) {
	if s.tracer == nil {
		s.tracer = trace.NewNoopTracerProvider().Tracer("github.com/iw2rmb/shiva")
	}

	startedAt := time.Now()
	statusCode := fiber.StatusInternalServerError
	requestID := requestIDFromContext(c)

	ctx := c.UserContext()
	if ctx == nil {
		ctx = c.Context()
	}
	ctx, span := s.tracer.Start(ctx, "webhook.ingest", trace.WithAttributes(
		attribute.String("http.route", "/internal/webhooks/gitlab"),
		attribute.String("request.id", requestID),
	))
	c.SetUserContext(ctx)
	defer func() {
		success := statusCode < 400
		if s.metrics != nil {
			s.metrics.ObserveIngest(time.Since(startedAt), success)
		}
		span.SetAttributes(attribute.Int("http.status_code", statusCode))
		if success {
			span.SetStatus(codes.Ok, "")
		} else {
			span.SetStatus(codes.Error, http.StatusText(statusCode))
		}
		span.End()
	}()

	logger := s.logger
	if logger != nil {
		logger = logger.With("request_id", requestID)
	}

	if strings.TrimSpace(s.cfg.GitLabWebhookSecret) == "" {
		statusCode = fiber.StatusServiceUnavailable
		return c.Status(statusCode).JSON(fiber.Map{
			"error": "gitlab webhook secret is not configured",
		})
	}

	headerToken := strings.TrimSpace(c.Get("X-Gitlab-Token"))
	if headerToken == "" {
		statusCode = fiber.StatusUnauthorized
		return c.Status(statusCode).JSON(fiber.Map{
			"error": "missing X-Gitlab-Token header",
		})
	}
	if !secureEqual(headerToken, s.cfg.GitLabWebhookSecret) {
		statusCode = fiber.StatusForbidden
		return c.Status(statusCode).JSON(fiber.Map{
			"error": "invalid X-Gitlab-Token",
		})
	}

	body := c.Body()
	if len(body) == 0 || !json.Valid(body) {
		statusCode = fiber.StatusBadRequest
		return c.Status(statusCode).JSON(fiber.Map{
			"error": "request body must be valid JSON",
		})
	}

	var payload gitLabWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		statusCode = fiber.StatusBadRequest
		return c.Status(statusCode).JSON(fiber.Map{
			"error": "failed to parse GitLab webhook payload",
		})
	}

	deliveryID := extractDeliveryID(c)
	if deliveryID == "" {
		statusCode = fiber.StatusBadRequest
		return c.Status(statusCode).JSON(fiber.Map{
			"error": "missing GitLab delivery id header",
		})
	}
	span.SetAttributes(attribute.String("delivery.id", deliveryID))

	eventType := strings.TrimSpace(c.Get("X-Gitlab-Event"))
	if eventType == "" {
		eventType = strings.TrimSpace(payload.ObjectKind)
	}

	sha := strings.TrimSpace(payload.After)
	if sha == "" || isZeroSHA(sha) {
		statusCode = fiber.StatusBadRequest
		return c.Status(statusCode).JSON(fiber.Map{
			"error": "payload.after must contain a commit sha",
		})
	}
	span.SetAttributes(attribute.String("revision.sha", sha))

	branch, err := parseBranchRef(payload.Ref)
	if err != nil {
		statusCode = fiber.StatusBadRequest
		return c.Status(statusCode).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	parentSha := strings.TrimSpace(payload.Before)
	if isZeroSHA(parentSha) {
		parentSha = ""
	}

	result, err := s.gitlabIngestor.PersistGitLabWebhook(ctx, store.GitLabIngestInput{
		GitLabProjectID:   payload.Project.ID,
		PathWithNamespace: payload.Project.PathWithNamespace,
		DefaultBranch:     payload.Project.DefaultBranch,
		Sha:               sha,
		Branch:            branch,
		ParentSha:         parentSha,
		EventType:         eventType,
		DeliveryID:        deliveryID,
		PayloadJSON:       body,
	})
	if err != nil {
		span.RecordError(err)
		switch {
		case errors.Is(err, store.ErrStoreNotConfigured):
			statusCode = fiber.StatusServiceUnavailable
			return c.Status(statusCode).JSON(fiber.Map{
				"error": "database is not configured",
			})
		case errors.Is(err, store.ErrInvalidIngestInput):
			statusCode = fiber.StatusBadRequest
			return c.Status(statusCode).JSON(fiber.Map{
				"error": err.Error(),
			})
		default:
			if logger != nil {
				logger.Error(
					"failed to persist GitLab ingest event",
					"delivery_id", deliveryID,
					"sha", textutil.ShortSHA(sha),
					"error", err,
				)
			}
			statusCode = fiber.StatusInternalServerError
			return c.Status(statusCode).JSON(fiber.Map{
				"error": "failed to persist webhook event",
			})
		}
	}

	span.SetAttributes(attribute.Int64("repo.id", result.RepoID))

	statusCode = fiber.StatusAccepted
	if result.Duplicate {
		statusCode = fiber.StatusOK
	}

	if logger != nil {
		logger.Info(
			"gitlab webhook accepted",
			"ingest_event_id", result.EventID,
			"duplicate", result.Duplicate,
			"delivery_id", deliveryID,
			"repo_id", result.RepoID,
			"sha", textutil.ShortSHA(sha),
		)
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

func parseBranchRef(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	const prefix = "refs/heads/"
	if !strings.HasPrefix(ref, prefix) || len(ref) == len(prefix) {
		return "", errors.New("payload.ref must be a branch ref in refs/heads/* format")
	}

	branch := strings.TrimSpace(strings.TrimPrefix(ref, prefix))
	if branch == "" {
		return "", errors.New("payload.ref must include a branch name")
	}

	return branch, nil
}

func isZeroSHA(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, char := range value {
		if char != '0' {
			return false
		}
	}
	return true
}
