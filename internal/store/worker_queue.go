package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iw2rmb/shiva/internal/store/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type IngestQueueEvent struct {
	ID           int64
	RepoID       int64
	Sha          string
	Branch       string
	ParentSha    string
	AttemptCount int32
}

func (s *Store) ClaimNextIngestEvent(ctx context.Context) (IngestQueueEvent, bool, error) {
	if s == nil || !s.configured || s.pool == nil {
		return IngestQueueEvent{}, false, ErrStoreNotConfigured
	}

	event, err := sqlc.New(s.pool).ClaimNextIngestEvent(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return IngestQueueEvent{}, false, nil
		}
		return IngestQueueEvent{}, false, fmt.Errorf("claim next ingest event: %w", err)
	}

	return mapQueueEvent(event), true, nil
}

func (s *Store) MarkIngestEventProcessed(ctx context.Context, eventID int64) error {
	if s == nil || !s.configured || s.pool == nil {
		return ErrStoreNotConfigured
	}
	if _, err := sqlc.New(s.pool).MarkIngestEventProcessed(ctx, eventID); err != nil {
		return fmt.Errorf("mark ingest event processed: %w", err)
	}
	return nil
}

func (s *Store) ScheduleIngestEventRetry(ctx context.Context, eventID int64, nextRetryAt time.Time, errorMessage string) error {
	if s == nil || !s.configured || s.pool == nil {
		return ErrStoreNotConfigured
	}

	if _, err := sqlc.New(s.pool).ScheduleIngestEventRetry(ctx, sqlc.ScheduleIngestEventRetryParams{
		ID:          eventID,
		Error:       strings.TrimSpace(errorMessage),
		NextRetryAt: pgtype.Timestamptz{Time: nextRetryAt.UTC(), Valid: true},
	}); err != nil {
		return fmt.Errorf("schedule ingest event retry: %w", err)
	}
	return nil
}

func (s *Store) MarkIngestEventFailed(ctx context.Context, eventID int64, errorMessage string) error {
	if s == nil || !s.configured || s.pool == nil {
		return ErrStoreNotConfigured
	}

	if _, err := sqlc.New(s.pool).MarkIngestEventFailed(ctx, sqlc.MarkIngestEventFailedParams{
		ID:    eventID,
		Error: strings.TrimSpace(errorMessage),
	}); err != nil {
		return fmt.Errorf("mark ingest event failed: %w", err)
	}
	return nil
}

func (s *Store) UpsertRevisionFromIngestEvent(ctx context.Context, event IngestQueueEvent) (int64, error) {
	if s == nil || !s.configured || s.pool == nil {
		return 0, ErrStoreNotConfigured
	}

	revision, err := sqlc.New(s.pool).CreateRevision(ctx, sqlc.CreateRevisionParams{
		RepoID:    event.RepoID,
		Sha:       event.Sha,
		Branch:    event.Branch,
		ParentSha: nullableText(event.ParentSha),
	})
	if err != nil {
		return 0, fmt.Errorf("upsert revision for ingest event %d: %w", event.ID, err)
	}

	return revision.ID, nil
}

func mapQueueEvent(event sqlc.IngestEvent) IngestQueueEvent {
	mapped := IngestQueueEvent{
		ID:           event.ID,
		RepoID:       event.RepoID,
		Sha:          event.Sha,
		Branch:       event.Branch,
		AttemptCount: event.AttemptCount,
	}
	if event.ParentSha.Valid {
		mapped.ParentSha = event.ParentSha.String
	}
	return mapped
}
