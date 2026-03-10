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
	DeliveryID   string
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

func (s *Store) MarkIngestEventProcessed(ctx context.Context, eventID int64, openapiChanged bool) error {
	if s == nil || !s.configured || s.pool == nil {
		return ErrStoreNotConfigured
	}
	if _, err := sqlc.New(s.pool).MarkIngestEventProcessed(ctx, sqlc.MarkIngestEventProcessedParams{
		ID:             eventID,
		OpenapiChanged: pgtype.Bool{Bool: openapiChanged, Valid: true},
	}); err != nil {
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

func mapQueueEvent(event sqlc.IngestEvent) IngestQueueEvent {
	mapped := IngestQueueEvent{
		ID:           event.ID,
		RepoID:       event.RepoID,
		DeliveryID:   event.DeliveryID,
		Sha:          event.Sha,
		Branch:       event.Branch,
		AttemptCount: event.AttemptCount,
	}
	if event.ParentSha.Valid {
		mapped.ParentSha = event.ParentSha.String
	}
	return mapped
}
