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

const (
	DeliveryEventTypeSpecUpdatedFull = "spec.updated.full"
	DeliveryEventTypeSpecUpdatedDiff = "spec.updated.diff"
)

const (
	DeliveryAttemptStatusPending        = "pending"
	DeliveryAttemptStatusRetryScheduled = "retry_scheduled"
	DeliveryAttemptStatusSucceeded      = "succeeded"
	DeliveryAttemptStatusFailed         = "failed"
)

type DeliveryAttempt struct {
	ID             int64
	SubscriptionID int64
	APISpecID      int64
	IngestEventID  int64
	EventType      string
	AttemptNo      int32
	Status         string
	ResponseCode   *int32
	Error          string
	NextRetryAt    *time.Time
}

type CreateDeliveryAttemptInput struct {
	SubscriptionID int64
	APISpecID      int64
	IngestEventID  int64
	EventType      string
	AttemptNo      int32
	Status         string
	NextRetryAt    *time.Time
}

type UpdateDeliveryAttemptResultInput struct {
	ID           int64
	Status       string
	ResponseCode *int32
	Error        string
	NextRetryAt  *time.Time
}

func (s *Store) CreateDeliveryAttempt(ctx context.Context, input CreateDeliveryAttemptInput) (DeliveryAttempt, error) {
	if s == nil || !s.configured || s.pool == nil {
		return DeliveryAttempt{}, ErrStoreNotConfigured
	}
	if input.SubscriptionID < 1 {
		return DeliveryAttempt{}, errors.New("subscription id must be positive")
	}
	if input.APISpecID < 1 {
		return DeliveryAttempt{}, errors.New("api spec id must be positive")
	}
	if input.IngestEventID < 1 {
		return DeliveryAttempt{}, errors.New("ingest event id must be positive")
	}
	if input.AttemptNo < 1 {
		return DeliveryAttempt{}, errors.New("attempt_no must be positive")
	}

	row, err := sqlc.New(s.pool).CreateDeliveryAttempt(ctx, sqlc.CreateDeliveryAttemptParams{
		SubscriptionID: input.SubscriptionID,
		ApiSpecID:      input.APISpecID,
		IngestEventID:  input.IngestEventID,
		EventType:      strings.TrimSpace(input.EventType),
		AttemptNo:      input.AttemptNo,
		Status:         strings.TrimSpace(input.Status),
		NextRetryAt:    nullableTimestamp(input.NextRetryAt),
	})
	if err != nil {
		return DeliveryAttempt{}, fmt.Errorf(
			"create delivery attempt for subscription %d api_spec_id=%d ingest_event_id %d event %q attempt_no=%d: %w",
			input.SubscriptionID,
			input.APISpecID,
			input.IngestEventID,
			input.EventType,
			input.AttemptNo,
			err,
		)
	}

	return mapDeliveryAttempt(row), nil
}

func (s *Store) UpdateDeliveryAttemptResult(
	ctx context.Context,
	input UpdateDeliveryAttemptResultInput,
) (DeliveryAttempt, error) {
	if s == nil || !s.configured || s.pool == nil {
		return DeliveryAttempt{}, ErrStoreNotConfigured
	}
	if input.ID < 1 {
		return DeliveryAttempt{}, errors.New("delivery attempt id must be positive")
	}

	responseCode := pgtype.Int4{}
	if input.ResponseCode != nil {
		responseCode = pgtype.Int4{Int32: *input.ResponseCode, Valid: true}
	}

	row, err := sqlc.New(s.pool).UpdateDeliveryAttemptResult(ctx, sqlc.UpdateDeliveryAttemptResultParams{
		ID:           input.ID,
		Status:       strings.TrimSpace(input.Status),
		ResponseCode: responseCode,
		Error:        strings.TrimSpace(input.Error),
		NextRetryAt:  nullableTimestamp(input.NextRetryAt),
	})
	if err != nil {
		return DeliveryAttempt{}, fmt.Errorf("update delivery attempt result id=%d: %w", input.ID, err)
	}

	return mapDeliveryAttempt(row), nil
}

func (s *Store) GetLatestDeliveryAttemptByKey(
	ctx context.Context,
	subscriptionID int64,
	apiSpecID int64,
	ingestEventID int64,
	eventType string,
) (DeliveryAttempt, bool, error) {
	if s == nil || !s.configured || s.pool == nil {
		return DeliveryAttempt{}, false, ErrStoreNotConfigured
	}
	if subscriptionID < 1 {
		return DeliveryAttempt{}, false, errors.New("subscription id must be positive")
	}
	if apiSpecID < 1 {
		return DeliveryAttempt{}, false, errors.New("api spec id must be positive")
	}
	if ingestEventID < 1 {
		return DeliveryAttempt{}, false, errors.New("ingest event id must be positive")
	}
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return DeliveryAttempt{}, false, errors.New("event type must not be empty")
	}

	row, err := sqlc.New(s.pool).GetLatestDeliveryAttemptByKey(ctx, sqlc.GetLatestDeliveryAttemptByKeyParams{
		SubscriptionID: subscriptionID,
		ApiSpecID:      apiSpecID,
		IngestEventID:  ingestEventID,
		EventType:      eventType,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DeliveryAttempt{}, false, nil
		}
		return DeliveryAttempt{}, false, fmt.Errorf(
			"get latest delivery attempt for subscription %d api_spec_id=%d ingest_event_id %d event %q: %w",
			subscriptionID,
			apiSpecID,
			ingestEventID,
			eventType,
			err,
		)
	}

	return mapDeliveryAttempt(row), true, nil
}

func mapDeliveryAttempt(row sqlc.DeliveryAttempt) DeliveryAttempt {
	mapped := DeliveryAttempt{
		ID:             row.ID,
		SubscriptionID: row.SubscriptionID,
		APISpecID:      row.ApiSpecID,
		IngestEventID:  row.IngestEventID,
		EventType:      row.EventType,
		AttemptNo:      row.AttemptNo,
		Status:         row.Status,
		Error:          row.Error,
	}
	if row.ResponseCode.Valid {
		value := row.ResponseCode.Int32
		mapped.ResponseCode = &value
	}
	if row.NextRetryAt.Valid {
		value := row.NextRetryAt.Time.UTC()
		mapped.NextRetryAt = &value
	}
	return mapped
}

func nullableTimestamp(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}
