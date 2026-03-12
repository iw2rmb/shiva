package httpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/gofiber/fiber/v2"

	"github.com/iw2rmb/shiva/internal/store"
)

type runtimeResolvedOperation struct {
	Route     runtimeRoute
	Snapshot  store.ResolvedReadSnapshot
	Candidate store.OperationSnapshot
	Document  *openapi3.T
	PathItem  *openapi3.PathItem
	Operation *openapi3.Operation
}

type runtimeOperationAmbiguityError struct {
	Candidates []store.OperationSnapshot
}

func (e *runtimeOperationAmbiguityError) Error() string {
	if e == nil {
		return "runtime route is ambiguous across APIs"
	}
	return "runtime route is ambiguous across APIs"
}

func (s *Server) handleRuntimeRoute(c *fiber.Ctx) error {
	route, err := parseRuntimeRoute(c.Context(), c.Path(), s.readStore)
	if err != nil {
		return s.writeQueryError(c, err)
	}

	resolved, err := s.resolveRuntimeOperation(c.Context(), c.Method(), route)
	if err != nil {
		var ambiguityErr *runtimeOperationAmbiguityError
		if errors.As(err, &ambiguityErr) {
			return writeOperationAmbiguity(c, ambiguityErr.Error(), ambiguityErr.Candidates)
		}
		return s.writeQueryError(c, err)
	}

	validated, err := validateRuntimeRequest(c.Context(), c, resolved)
	if err != nil {
		var failure *runtimeFailure
		if errors.As(err, &failure) {
			if responseErr := writeRuntimeFailureResponse(c, resolved, failure); responseErr != nil {
				return s.writeQueryError(c, responseErr)
			}
			return nil
		}
		return s.writeQueryError(c, err)
	}

	response, err := buildRuntimeSuccessResponse(c.Context(), validated, resolved, c.Get(fiber.HeaderAccept))
	if err != nil {
		var failure *runtimeFailure
		if errors.As(err, &failure) {
			if responseErr := writeRuntimeFailureResponse(c, resolved, failure); responseErr != nil {
				return s.writeQueryError(c, responseErr)
			}
			return nil
		}
		return s.writeQueryError(c, err)
	}

	return writeRuntimePreparedResponse(c, response)
}

func (s *Server) resolveRuntimeOperation(
	ctx context.Context,
	method string,
	route runtimeRoute,
) (runtimeResolvedOperation, error) {
	method = normalizeHTTPMethod(method)
	if method == "" {
		return runtimeResolvedOperation{}, invalidQuery("runtime method is not supported")
	}

	snapshot, err := s.readStore.ResolveReadSnapshot(ctx, route.SnapshotInput)
	if err != nil {
		return runtimeResolvedOperation{}, err
	}

	resolved, err := s.readStore.ResolveOperationCandidatesByMethodPath(
		ctx,
		store.ResolveOperationByMethodPathInput{
			ResolveReadSnapshotInput: store.ResolveReadSnapshotInput{
				Namespace:  snapshot.Repo.Namespace,
				Repo:       snapshot.Repo.Repo,
				RevisionID: snapshot.Revision.ID,
			},
			Method: method,
			Path:   route.OpenAPIPath,
		},
	)
	if err != nil {
		return runtimeResolvedOperation{}, err
	}

	switch len(resolved.Candidates) {
	case 0:
		return runtimeResolvedOperation{}, fmt.Errorf(
			"%w: repo=%q revision_id=%d method=%s path=%s",
			errOperationNotFound,
			snapshot.Repo.Path(),
			snapshot.Revision.ID,
			method,
			route.OpenAPIPath,
		)
	case 1:
	default:
		return runtimeResolvedOperation{}, &runtimeOperationAmbiguityError{
			Candidates: resolved.Candidates,
		}
	}

	candidate := resolved.Candidates[0]
	document, err := s.runtimeSpecs.GetOrLoad(ctx, candidate.APISpecRevisionID, s.readStore.GetSpecArtifactByAPISpecRevisionID)
	if err != nil {
		return runtimeResolvedOperation{}, err
	}

	pathItem, operation, err := resolveRuntimeDocumentOperation(document, candidate.Method, candidate.Path)
	if err != nil {
		return runtimeResolvedOperation{}, fmt.Errorf(
			"resolve runtime operation in parsed spec for api_spec_revision_id=%d method=%s path=%s: %w",
			candidate.APISpecRevisionID,
			candidate.Method,
			candidate.Path,
			err,
		)
	}

	return runtimeResolvedOperation{
		Route:     route,
		Snapshot:  snapshot,
		Candidate: candidate,
		Document:  document,
		PathItem:  pathItem,
		Operation: operation,
	}, nil
}

func resolveRuntimeDocumentOperation(
	document *openapi3.T,
	method string,
	path string,
) (*openapi3.PathItem, *openapi3.Operation, error) {
	if document == nil {
		return nil, nil, fmt.Errorf("document must not be nil")
	}
	if document.Paths == nil {
		return nil, nil, fmt.Errorf("document paths must not be nil")
	}

	pathItem := document.Paths.Find(path)
	if pathItem == nil {
		return nil, nil, fmt.Errorf("path %q is not present in spec document", path)
	}

	operation := pathItem.GetOperation(strings.ToUpper(method))
	if operation == nil {
		return nil, nil, fmt.Errorf("method %q is not present for path %q in spec document", method, path)
	}

	return pathItem, operation, nil
}
