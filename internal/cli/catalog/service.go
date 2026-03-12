package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iw2rmb/shiva/internal/cli/request"
)

const (
	defaultFloatingTTL = 30 * time.Second
	defaultReposTTL    = 5 * time.Minute
)

type inventoryClient interface {
	ListRepos(ctx context.Context) ([]byte, error)
	GetCatalogStatus(ctx context.Context, repo string) ([]byte, error)
	ListAPIs(ctx context.Context, selector request.Envelope) ([]byte, error)
	ListOperations(ctx context.Context, selector request.Envelope) ([]byte, error)
}

type RefreshOptions struct {
	Refresh bool
	Offline bool
}

type PreparedSnapshot struct {
	Scope       Scope
	Fingerprint SnapshotFingerprint
}

type Service struct {
	store       *Store
	now         func() time.Time
	floatingTTL time.Duration
	reposTTL    time.Duration
}

type repoCatalogStatusPayload struct {
	Repo             string                  `json:"repo"`
	SnapshotRevision *snapshotRevisionRecord `json:"snapshot_revision"`
}

type snapshotRevisionRecord struct {
	ID  int64  `json:"id"`
	SHA string `json:"sha"`
}

func NewService(store *Store) *Service {
	return &Service{
		store:       store,
		now:         time.Now,
		floatingTTL: defaultFloatingTTL,
		reposTTL:    defaultReposTTL,
	}
}

func (s *Service) PrepareRepos(
	ctx context.Context,
	client inventoryClient,
	profile string,
	options RefreshOptions,
) error {
	if s == nil || s.store == nil {
		return errors.New("catalog service is not configured")
	}
	return s.ensureRepos(ctx, client, profile, options)
}

func (s *Service) PrepareAPIs(
	ctx context.Context,
	client inventoryClient,
	profile string,
	selector request.Envelope,
	options RefreshOptions,
) (PreparedSnapshot, error) {
	return s.prepare(ctx, client, profile, selector, options, false)
}

func (s *Service) PrepareOperations(
	ctx context.Context,
	client inventoryClient,
	profile string,
	selector request.Envelope,
	options RefreshOptions,
) (PreparedSnapshot, error) {
	return s.prepare(ctx, client, profile, selector, options, true)
}

func (s *Service) PrepareSpec(
	ctx context.Context,
	client inventoryClient,
	profile string,
	selector request.Envelope,
	options RefreshOptions,
) (PreparedSnapshot, error) {
	return s.prepare(ctx, client, profile, selector, options, false)
}

func (s *Service) PrepareOperation(
	ctx context.Context,
	client inventoryClient,
	profile string,
	selector request.Envelope,
	options RefreshOptions,
) (PreparedSnapshot, error) {
	return s.prepare(ctx, client, profile, selector, options, true)
}

func (s *Service) PrepareCall(
	ctx context.Context,
	client inventoryClient,
	profile string,
	selector request.Envelope,
	options RefreshOptions,
) (PreparedSnapshot, error) {
	return s.prepare(ctx, client, profile, selector, options, true)
}

func (s *Service) prepare(
	ctx context.Context,
	client inventoryClient,
	profile string,
	selector request.Envelope,
	options RefreshOptions,
	needOperations bool,
) (PreparedSnapshot, error) {
	if s == nil || s.store == nil {
		return PreparedSnapshot{}, errors.New("catalog service is not configured")
	}

	scope := ScopeFromSelector(selector.RevisionID, selector.SHA)
	if scope.Floating {
		if err := s.ensureRepos(ctx, client, profile, options); err != nil {
			return PreparedSnapshot{}, err
		}
		return s.prepareFloating(ctx, client, profile, selector, options, needOperations)
	}
	return s.preparePinned(ctx, client, profile, selector, options, scope, needOperations)
}

func (s *Service) ensureRepos(
	ctx context.Context,
	client inventoryClient,
	profile string,
	options RefreshOptions,
) error {
	record, found, err := s.store.LoadRepos(profile)
	if err != nil {
		return err
	}

	if options.Offline {
		if !found {
			return fmt.Errorf("offline cache miss: repo catalog for profile %q", profile)
		}
		return nil
	}

	if !options.Refresh && found && s.now().UTC().Sub(record.RefreshedAt) < s.reposTTL {
		return nil
	}

	payload, err := client.ListRepos(ctx)
	if err != nil {
		if found && !options.Refresh {
			return nil
		}
		return err
	}
	return s.store.SaveRepos(profile, payload)
}

func (s *Service) prepareFloating(
	ctx context.Context,
	client inventoryClient,
	profile string,
	selector request.Envelope,
	options RefreshOptions,
	needOperations bool,
) (PreparedSnapshot, error) {
	repoKey := selector.RepoPath()
	statusRecord, foundStatus, err := s.store.LoadStatus(profile, repoKey)
	if err != nil {
		return PreparedSnapshot{}, err
	}

	statusStale := !foundStatus || statusRecord.CheckedAt == nil || s.now().UTC().Sub(*statusRecord.CheckedAt) >= s.floatingTTL
	if options.Offline {
		if !foundStatus {
			return PreparedSnapshot{}, fmt.Errorf("offline cache miss: catalog status for repo %q", selector.Repo)
		}
		if err := s.ensureCachedSlices(selector, profile, needOperations); err != nil {
			return PreparedSnapshot{}, err
		}
		return PreparedSnapshot{
			Scope:       ScopeFromSelector(selector.RevisionID, selector.SHA),
			Fingerprint: statusRecord.Fingerprint,
		}, nil
	}

	if options.Refresh || statusStale {
		payload, err := client.GetCatalogStatus(ctx, repoKey)
		if err != nil {
			if !foundStatus || options.Refresh {
				return PreparedSnapshot{}, err
			}
			if err := s.ensureCachedSlices(selector, profile, needOperations); err != nil {
				return PreparedSnapshot{}, err
			}
			return PreparedSnapshot{
				Scope:       ScopeFromSelector(selector.RevisionID, selector.SHA),
				Fingerprint: statusRecord.Fingerprint,
			}, nil
		}

		fingerprint, err := parseStatusFingerprint(payload)
		if err != nil {
			return PreparedSnapshot{}, err
		}
		if err := s.store.SaveStatus(profile, repoKey, payload, fingerprint); err != nil {
			return PreparedSnapshot{}, err
		}
		statusRecord, foundStatus = Record{
			Kind:        kindStatus,
			Profile:     profile,
			Repo:        repoKey,
			Fingerprint: fingerprint,
			CheckedAt:   timePtr(s.now().UTC()),
			RefreshedAt: s.now().UTC(),
			Payload:     payload,
		}, true
	}

	scope := ScopeFromSelector(selector.RevisionID, selector.SHA)
	apisRecord, apisFound, err := s.store.LoadAPIs(profile, repoKey, scope)
	if err != nil {
		return PreparedSnapshot{}, err
	}
	needAPIsRefresh := options.Refresh || !apisFound || apisRecord.Fingerprint != statusRecord.Fingerprint
	if needAPIsRefresh {
		payload, err := client.ListAPIs(ctx, selector)
		if err != nil {
			if !apisFound || options.Refresh {
				return PreparedSnapshot{}, err
			}
		} else if err := s.store.SaveAPIs(profile, repoKey, scope, payload, statusRecord.Fingerprint); err != nil {
			return PreparedSnapshot{}, err
		}
	}

	if needOperations {
		opsRecord, opsFound, err := s.store.LoadOperations(profile, repoKey, selector.API, scope)
		if err != nil {
			return PreparedSnapshot{}, err
		}
		needOpsRefresh := options.Refresh || !opsFound || opsRecord.Fingerprint != statusRecord.Fingerprint
		if needOpsRefresh {
			payload, err := client.ListOperations(ctx, selector)
			if err != nil {
				if !opsFound || options.Refresh {
					return PreparedSnapshot{}, err
				}
			} else if err := s.store.SaveOperations(profile, repoKey, selector.API, scope, payload, statusRecord.Fingerprint); err != nil {
				return PreparedSnapshot{}, err
			}
		}
	}

	if !options.Refresh && foundStatus && statusStale {
		if err := s.store.TouchStatus(profile, repoKey, statusRecord); err != nil {
			return PreparedSnapshot{}, err
		}
	}

	return PreparedSnapshot{
		Scope:       scope,
		Fingerprint: statusRecord.Fingerprint,
	}, nil
}

func (s *Service) preparePinned(
	ctx context.Context,
	client inventoryClient,
	profile string,
	selector request.Envelope,
	options RefreshOptions,
	scope Scope,
	needOperations bool,
) (PreparedSnapshot, error) {
	repoKey := selector.RepoPath()
	fingerprint := SnapshotFingerprint{
		RevisionID: selector.RevisionID,
		SHA:        strings.TrimSpace(selector.SHA),
	}

	_, apisFound, err := s.store.LoadAPIs(profile, repoKey, scope)
	if err != nil {
		return PreparedSnapshot{}, err
	}
	if options.Offline && !apisFound {
		return PreparedSnapshot{}, fmt.Errorf("offline cache miss: api catalog for repo %q scope %q", selector.Repo, scope.Key)
	}
	if !options.Offline && (options.Refresh || !apisFound) {
		payload, err := client.ListAPIs(ctx, selector)
		if err != nil {
			if !apisFound || options.Refresh {
				return PreparedSnapshot{}, err
			}
		} else if err := s.store.SaveAPIs(profile, repoKey, scope, payload, fingerprint); err != nil {
			return PreparedSnapshot{}, err
		}
	}

	if needOperations {
		_, opsFound, err := s.store.LoadOperations(profile, repoKey, selector.API, scope)
		if err != nil {
			return PreparedSnapshot{}, err
		}
		if options.Offline && !opsFound {
			return PreparedSnapshot{}, fmt.Errorf(
				"offline cache miss: operation catalog for repo %q scope %q",
				selector.Repo,
				scope.Key,
			)
		}
		if !options.Offline && (options.Refresh || !opsFound) {
			payload, err := client.ListOperations(ctx, selector)
			if err != nil {
				if !opsFound || options.Refresh {
					return PreparedSnapshot{}, err
				}
			} else if err := s.store.SaveOperations(profile, repoKey, selector.API, scope, payload, fingerprint); err != nil {
				return PreparedSnapshot{}, err
			}
		}
	}

	return PreparedSnapshot{
		Scope:       scope,
		Fingerprint: fingerprint,
	}, nil
}

func (s *Service) ensureCachedSlices(
	selector request.Envelope,
	profile string,
	needOperations bool,
) error {
	scope := ScopeFromSelector(selector.RevisionID, selector.SHA)
	repoKey := selector.RepoPath()
	if _, found, err := s.store.LoadAPIs(profile, repoKey, scope); err != nil {
		return err
	} else if !found {
		return fmt.Errorf("offline cache miss: api catalog for repo %q", selector.Repo)
	}

	if needOperations {
		if _, found, err := s.store.LoadOperations(profile, repoKey, selector.API, scope); err != nil {
			return err
		} else if !found {
			return fmt.Errorf("offline cache miss: operation catalog for repo %q", selector.Repo)
		}
	}

	return nil
}

func parseStatusFingerprint(payload []byte) (SnapshotFingerprint, error) {
	var status repoCatalogStatusPayload
	if err := json.Unmarshal(payload, &status); err != nil {
		return SnapshotFingerprint{}, fmt.Errorf("decode catalog status: %w", err)
	}
	if status.SnapshotRevision == nil {
		return SnapshotFingerprint{}, nil
	}
	return SnapshotFingerprint{
		RevisionID: status.SnapshotRevision.ID,
		SHA:        strings.TrimSpace(status.SnapshotRevision.SHA),
	}, nil
}

func timePtr(value time.Time) *time.Time {
	return &value
}

func ReposRecordStale(record Record, now time.Time) bool {
	return now.UTC().Sub(record.RefreshedAt) >= defaultReposTTL
}

func StatusRecordStale(record Record, now time.Time) bool {
	return record.CheckedAt == nil || now.UTC().Sub(*record.CheckedAt) >= defaultFloatingTTL
}
