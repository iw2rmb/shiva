package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/iw2rmb/shiva/internal/store"
)

const (
	HeaderTimestamp = "X-Shiva-Timestamp"
	HeaderSignature = "X-Shiva-Signature"
)

type Store interface {
	ListEnabledSubscriptionsByRepo(ctx context.Context, tenantID, repoID int64) ([]store.Subscription, error)
	GetLatestDeliveryAttemptByKey(
		ctx context.Context,
		subscriptionID int64,
		revisionID int64,
		eventType string,
	) (store.DeliveryAttempt, bool, error)
	CreateDeliveryAttempt(ctx context.Context, input store.CreateDeliveryAttemptInput) (store.DeliveryAttempt, error)
	UpdateDeliveryAttemptResult(
		ctx context.Context,
		input store.UpdateDeliveryAttemptResultInput,
	) (store.DeliveryAttempt, error)
}

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type Option func(*Notifier)

type Notifier struct {
	store      Store
	httpClient HTTPClient
	now        func() time.Time
	sleep      func(context.Context, time.Duration) error
}

type RevisionNotification struct {
	TenantID    int64
	TenantKey   string
	RepoID      int64
	RepoPath    string
	RevisionID  int64
	Sha         string
	Branch      string
	ProcessedAt time.Time
	Artifact    store.SpecArtifact
	SpecChange  store.SpecChange
	FromSHA     string
}

type eventEnvelope[T any] struct {
	Type        string `json:"type"`
	EventID     string `json:"event_id"`
	Tenant      string `json:"tenant"`
	Repo        string `json:"repo"`
	RevisionID  int64  `json:"revision_id"`
	Sha         string `json:"sha"`
	Branch      string `json:"branch"`
	ProcessedAt string `json:"processed_at"`
	Payload     T      `json:"payload"`
}

type fullEventPayload struct {
	ETag      string          `json:"etag"`
	SizeBytes int64           `json:"size_bytes"`
	SpecJSON  json.RawMessage `json:"spec_json"`
	SpecYAML  string          `json:"spec_yaml"`
}

type diffEventPayload struct {
	FromRevisionID *int64          `json:"from_revision_id,omitempty"`
	FromSHA        string          `json:"from_sha,omitempty"`
	ToRevisionID   int64           `json:"to_revision_id"`
	ToSHA          string          `json:"to_sha"`
	Changes        json.RawMessage `json:"changes"`
}

type builtEvent struct {
	eventType string
	body      []byte
}

type deliveryOutcome struct {
	success      bool
	retryable    bool
	responseCode *int32
	errorMessage string
}

func New(store Store, options ...Option) *Notifier {
	notifier := &Notifier{
		store:      store,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		now:        time.Now,
		sleep:      sleepContext,
	}

	for _, option := range options {
		option(notifier)
	}

	return notifier
}

func WithHTTPClient(httpClient HTTPClient) Option {
	return func(n *Notifier) {
		if httpClient != nil {
			n.httpClient = httpClient
		}
	}
}

func WithNow(now func() time.Time) Option {
	return func(n *Notifier) {
		if now != nil {
			n.now = now
		}
	}
}

func WithSleep(sleep func(context.Context, time.Duration) error) Option {
	return func(n *Notifier) {
		if sleep != nil {
			n.sleep = sleep
		}
	}
}

func (n *Notifier) NotifyRevision(ctx context.Context, notification RevisionNotification) error {
	if n == nil {
		return errors.New("notifier is nil")
	}
	if n.store == nil {
		return errors.New("notifier store is not configured")
	}
	if n.httpClient == nil {
		return errors.New("notifier http client is not configured")
	}
	if notification.TenantID < 1 {
		return errors.New("tenant id must be positive")
	}
	if notification.RepoID < 1 {
		return errors.New("repo id must be positive")
	}
	if notification.RevisionID < 1 {
		return errors.New("revision id must be positive")
	}
	if notification.ProcessedAt.IsZero() {
		notification.ProcessedAt = n.now().UTC()
	}

	subscriptions, err := n.store.ListEnabledSubscriptionsByRepo(ctx, notification.TenantID, notification.RepoID)
	if err != nil {
		return fmt.Errorf("list enabled subscriptions: %w", err)
	}
	if len(subscriptions) == 0 {
		return nil
	}

	events, err := buildEvents(notification)
	if err != nil {
		return err
	}

	for _, subscription := range subscriptions {
		for _, event := range events {
			if err := n.dispatchEvent(ctx, subscription, notification.RevisionID, event); err != nil {
				return err
			}
		}
	}

	return nil
}

func buildEvents(notification RevisionNotification) ([]builtEvent, error) {
	processedAt := notification.ProcessedAt.UTC()
	processedAtText := processedAt.Format(time.RFC3339Nano)
	tenantKey := strings.TrimSpace(notification.TenantKey)
	repoPath := strings.TrimSpace(notification.RepoPath)

	fullEnvelope := eventEnvelope[fullEventPayload]{
		Type:        store.DeliveryEventTypeSpecUpdatedFull,
		EventID:     deterministicEnvelopeEventID(notification.RevisionID, store.DeliveryEventTypeSpecUpdatedFull),
		Tenant:      tenantKey,
		Repo:        repoPath,
		RevisionID:  notification.RevisionID,
		Sha:         notification.Sha,
		Branch:      notification.Branch,
		ProcessedAt: processedAtText,
		Payload: fullEventPayload{
			ETag:      notification.Artifact.ETag,
			SizeBytes: notification.Artifact.SizeBytes,
			SpecJSON:  json.RawMessage(notification.Artifact.SpecJSON),
			SpecYAML:  notification.Artifact.SpecYAML,
		},
	}

	diffEnvelope := eventEnvelope[diffEventPayload]{
		Type:        store.DeliveryEventTypeSpecUpdatedDiff,
		EventID:     deterministicEnvelopeEventID(notification.RevisionID, store.DeliveryEventTypeSpecUpdatedDiff),
		Tenant:      tenantKey,
		Repo:        repoPath,
		RevisionID:  notification.RevisionID,
		Sha:         notification.Sha,
		Branch:      notification.Branch,
		ProcessedAt: processedAtText,
		Payload: diffEventPayload{
			FromRevisionID: notification.SpecChange.FromRevisionID,
			FromSHA:        strings.TrimSpace(notification.FromSHA),
			ToRevisionID:   notification.RevisionID,
			ToSHA:          notification.Sha,
			Changes:        json.RawMessage(notification.SpecChange.ChangeJSON),
		},
	}

	fullBodyWithoutID, err := json.Marshal(fullEnvelope)
	if err != nil {
		return nil, fmt.Errorf("marshal full event payload: %w", err)
	}
	diffBodyWithoutID, err := json.Marshal(diffEnvelope)
	if err != nil {
		return nil, fmt.Errorf("marshal diff event payload: %w", err)
	}
	// Validate canonical payload JSON before dispatch.
	if !json.Valid(fullBodyWithoutID) {
		return nil, errors.New("full event payload is invalid json")
	}
	if !json.Valid(diffBodyWithoutID) {
		return nil, errors.New("diff event payload is invalid json")
	}

	return []builtEvent{
		{
			eventType: store.DeliveryEventTypeSpecUpdatedFull,
			body:      fullBodyWithoutID,
		},
		{
			eventType: store.DeliveryEventTypeSpecUpdatedDiff,
			body:      diffBodyWithoutID,
		},
	}, nil
}

func (n *Notifier) dispatchEvent(
	ctx context.Context,
	subscription store.Subscription,
	revisionID int64,
	event builtEvent,
) error {
	latestAttempt, hasAttempt, err := n.store.GetLatestDeliveryAttemptByKey(
		ctx,
		subscription.ID,
		revisionID,
		event.eventType,
	)
	if err != nil {
		return fmt.Errorf(
			"load latest delivery attempt for subscription %d revision %d event %q: %w",
			subscription.ID,
			revisionID,
			event.eventType,
			err,
		)
	}

	if hasAttempt && isTerminalStatus(latestAttempt.Status) {
		return nil
	}

	if hasAttempt && latestAttempt.Status == store.DeliveryAttemptStatusRetryScheduled && latestAttempt.NextRetryAt != nil {
		wait := latestAttempt.NextRetryAt.Sub(n.now().UTC())
		if wait > 0 {
			if err := n.sleep(ctx, wait); err != nil {
				return err
			}
		}
	}

	maxAttempts := normalizedMaxAttempts(subscription.MaxAttempts)
	attemptNo := int32(1)
	if hasAttempt {
		attemptNo = latestAttempt.AttemptNo + 1
	}
	if attemptNo > maxAttempts {
		if hasAttempt {
			if _, err := n.store.UpdateDeliveryAttemptResult(ctx, store.UpdateDeliveryAttemptResultInput{
				ID:     latestAttempt.ID,
				Status: store.DeliveryAttemptStatusFailed,
				Error:  "max attempts exhausted",
			}); err != nil {
				return fmt.Errorf("mark delivery attempt %d failed after max attempts exhausted: %w", latestAttempt.ID, err)
			}
		}
		return nil
	}

	for ; attemptNo <= maxAttempts; attemptNo++ {
		createdAttempt, err := n.store.CreateDeliveryAttempt(ctx, store.CreateDeliveryAttemptInput{
			SubscriptionID: subscription.ID,
			RevisionID:     revisionID,
			EventType:      event.eventType,
			AttemptNo:      attemptNo,
			Status:         store.DeliveryAttemptStatusPending,
		})
		if err != nil {
			return fmt.Errorf(
				"create delivery attempt for subscription %d revision %d event %q attempt_no=%d: %w",
				subscription.ID,
				revisionID,
				event.eventType,
				attemptNo,
				err,
			)
		}

		eventID := deterministicEventID(subscription.ID, revisionID, event.eventType)
		outcome := n.deliverOnce(ctx, subscription, eventID, event.body)
		if outcome.success {
			if _, err := n.store.UpdateDeliveryAttemptResult(ctx, store.UpdateDeliveryAttemptResultInput{
				ID:           createdAttempt.ID,
				Status:       store.DeliveryAttemptStatusSucceeded,
				ResponseCode: outcome.responseCode,
			}); err != nil {
				return fmt.Errorf("mark delivery attempt %d succeeded: %w", createdAttempt.ID, err)
			}
			return nil
		}

		if !outcome.retryable || attemptNo >= maxAttempts {
			if _, err := n.store.UpdateDeliveryAttemptResult(ctx, store.UpdateDeliveryAttemptResultInput{
				ID:           createdAttempt.ID,
				Status:       store.DeliveryAttemptStatusFailed,
				ResponseCode: outcome.responseCode,
				Error:        outcome.errorMessage,
			}); err != nil {
				return fmt.Errorf("mark delivery attempt %d failed: %w", createdAttempt.ID, err)
			}
			return nil
		}

		nextRetryAt := n.now().UTC().Add(calculateBackoff(
			subscription.BackoffInitialSeconds,
			subscription.BackoffMaxSeconds,
			attemptNo,
		))
		if _, err := n.store.UpdateDeliveryAttemptResult(ctx, store.UpdateDeliveryAttemptResultInput{
			ID:           createdAttempt.ID,
			Status:       store.DeliveryAttemptStatusRetryScheduled,
			ResponseCode: outcome.responseCode,
			Error:        outcome.errorMessage,
			NextRetryAt:  &nextRetryAt,
		}); err != nil {
			return fmt.Errorf("schedule retry for delivery attempt %d: %w", createdAttempt.ID, err)
		}

		wait := nextRetryAt.Sub(n.now().UTC())
		if wait > 0 {
			if err := n.sleep(ctx, wait); err != nil {
				return err
			}
		}
	}

	return nil
}

func (n *Notifier) deliverOnce(
	ctx context.Context,
	subscription store.Subscription,
	eventID string,
	body []byte,
) deliveryOutcome {
	secret := strings.TrimSpace(subscription.Secret)
	if secret == "" {
		return deliveryOutcome{
			retryable:    false,
			errorMessage: "subscription secret is empty",
		}
	}
	targetURL := strings.TrimSpace(subscription.TargetURL)
	if targetURL == "" {
		return deliveryOutcome{
			retryable:    false,
			errorMessage: "subscription target_url is empty",
		}
	}
	if err := validateTargetURL(targetURL); err != nil {
		return deliveryOutcome{
			retryable:    false,
			errorMessage: err.Error(),
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return deliveryOutcome{
			retryable:    false,
			errorMessage: fmt.Sprintf("build request: %v", err),
		}
	}

	timestamp := n.now().UTC().Format(time.RFC3339Nano)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(HeaderTimestamp, timestamp)
	req.Header.Set(HeaderSignature, signPayload(secret, body))
	req.Header.Set("User-Agent", "shiva-notifier/1")
	req.Header.Set("X-Shiva-Event-ID", eventID)

	response, err := n.httpClient.Do(req)
	if err != nil {
		return deliveryOutcome{
			retryable:    true,
			errorMessage: fmt.Sprintf("dispatch request: %v", err),
		}
	}
	defer response.Body.Close()

	responseCode := int32(response.StatusCode)
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return deliveryOutcome{
			success:      true,
			retryable:    false,
			responseCode: &responseCode,
		}
	}

	const maxBodyBytes = 512
	bodyReader := io.LimitReader(response.Body, maxBodyBytes)
	responseBody, _ := io.ReadAll(bodyReader)
	message := strings.TrimSpace(string(responseBody))
	if message == "" {
		message = http.StatusText(response.StatusCode)
	}

	return deliveryOutcome{
		retryable:    true,
		responseCode: &responseCode,
		errorMessage: fmt.Sprintf("non-2xx response (%d): %s", response.StatusCode, message),
	}
}

func isTerminalStatus(status string) bool {
	return status == store.DeliveryAttemptStatusSucceeded || status == store.DeliveryAttemptStatusFailed
}

func deterministicEventID(subscriptionID int64, revisionID int64, eventType string) string {
	return fmt.Sprintf("sub:%d:rev:%d:event:%s", subscriptionID, revisionID, eventType)
}

func deterministicEnvelopeEventID(revisionID int64, eventType string) string {
	return fmt.Sprintf("rev:%d:event:%s", revisionID, eventType)
}

func signPayload(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func calculateBackoff(initialSeconds int32, maxSeconds int32, attemptNo int32) time.Duration {
	initial := time.Duration(initialSeconds) * time.Second
	if initial <= 0 {
		initial = time.Second
	}

	max := time.Duration(maxSeconds) * time.Second
	if max <= 0 {
		max = 30 * time.Second
	}
	if max < initial {
		max = initial
	}

	if attemptNo <= 1 {
		return initial
	}

	delay := initial
	for i := int32(1); i < attemptNo; i++ {
		if delay >= max {
			return max
		}
		next := delay * 2
		if next <= 0 || next > max {
			return max
		}
		delay = next
	}
	return delay
}

func normalizedMaxAttempts(maxAttempts int32) int32 {
	if maxAttempts < 1 {
		return 1
	}
	return maxAttempts
}

func validateTargetURL(target string) error {
	parsed, err := url.ParseRequestURI(target)
	if err != nil {
		return fmt.Errorf("invalid subscription target_url: %w", err)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		return nil
	default:
		return fmt.Errorf("subscription target_url scheme must be http or https")
	}
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return ctx.Err()
	}

	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
