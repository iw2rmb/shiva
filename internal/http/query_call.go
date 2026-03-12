package httpserver

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/gofiber/fiber/v2"

	"github.com/iw2rmb/shiva/internal/cli/executor"
	"github.com/iw2rmb/shiva/internal/cli/request"
)

func (s *Server) handlePostCall(c *fiber.Ctx) error {
	envelope, err := parseCallEnvelope(c)
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
	default:
		return writeOperationAmbiguity(c, "call request is ambiguous", resolved.Candidates)
	}

	normalized := envelope
	normalized.API = resolved.Candidates[0].API
	normalized.RevisionID = resolved.Snapshot.Revision.ID
	normalized.SHA = resolved.Snapshot.Revision.Sha
	normalized.OperationID = resolved.Candidates[0].OperationID
	normalized.Method = resolved.Candidates[0].Method
	normalized.Path = resolved.Candidates[0].Path

	plan, err := executor.PlanShivaCall(normalized)
	if err != nil {
		return s.writeQueryError(c, err)
	}

	return c.Status(fiber.StatusOK).JSON(plan)
}

func parseCallEnvelope(c *fiber.Ctx) (request.Envelope, error) {
	var envelope request.Envelope
	if err := decodeJSONObjectBody(c.Body(), &envelope); err != nil {
		return request.Envelope{}, err
	}

	normalized, err := request.NormalizeCallEnvelope(envelope, request.NormalizeCallOptions{
		DefaultTarget:    request.DefaultShivaTarget,
		AllowMissingKind: true,
	})
	if err != nil {
		return request.Envelope{}, err
	}
	if normalized.Target != "" && normalized.Target != request.DefaultShivaTarget {
		return request.Envelope{}, invalidQuery(`target must be "shiva" for this endpoint`)
	}
	return normalized, nil
}

func decodeJSONObjectBody(body []byte, target any) error {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return invalidQuery("request body must not be empty")
	}
	if trimmed[0] != '{' {
		return invalidQuery("request body must be a json object")
	}

	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return invalidQuery(fmt.Sprintf("invalid request body: %v", err))
	}

	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err == nil {
		return invalidQuery("request body must contain exactly one json object")
	}

	return nil
}
