package httpserver

import (
	"context"

	"github.com/iw2rmb/shiva/internal/cli/request"
	"github.com/iw2rmb/shiva/internal/store"
)

func snapshotInputFromEnvelope(envelope request.Envelope) store.ResolveReadSnapshotInput {
	return store.ResolveReadSnapshotInput{
		Namespace:  envelope.Namespace,
		Repo:       envelope.Repo,
		APIPath:    envelope.API,
		RevisionID: envelope.RevisionID,
		SHA:        envelope.SHA,
	}
}

func (s *Server) resolveOperationCandidates(
	ctx context.Context,
	envelope request.Envelope,
) (store.ResolvedOperationCandidates, error) {
	if envelope.OperationID != "" {
		return s.readStore.ResolveOperationCandidatesByOperationID(
			ctx,
			store.ResolveOperationByIDInput{
				ResolveReadSnapshotInput: snapshotInputFromEnvelope(envelope),
				OperationID:              envelope.OperationID,
			},
		)
	}

	return s.readStore.ResolveOperationCandidatesByMethodPath(
		ctx,
		store.ResolveOperationByMethodPathInput{
			ResolveReadSnapshotInput: snapshotInputFromEnvelope(envelope),
			Method:                   envelope.Method,
			Path:                     envelope.Path,
		},
	)
}
