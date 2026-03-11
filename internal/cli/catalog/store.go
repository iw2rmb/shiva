package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	cacheDirName = "shiva"
	cacheVersion = "v1"

	kindRepos      = "repos"
	kindStatus     = "status"
	kindAPIs       = "apis"
	kindOperations = "operations"
	kindSpecJSON   = "response_spec_json"
	kindSpecYAML   = "response_spec_yaml"
	kindOperation  = "response_operation"
	kindCallPlan   = "response_call_plan"
	globalKey      = "__all__"
)

type Store struct {
	root string
	now  func() time.Time
}

type Scope struct {
	Key        string
	Floating   bool
	RevisionID int64
	SHA        string
}

type SnapshotFingerprint struct {
	RevisionID int64  `json:"revision_id,omitempty"`
	SHA        string `json:"sha,omitempty"`
}

type Record struct {
	Kind        string              `json:"kind"`
	Profile     string              `json:"profile"`
	Repo        string              `json:"repo,omitempty"`
	API         string              `json:"api,omitempty"`
	Scope       string              `json:"scope,omitempty"`
	SelectorKey string              `json:"selector_key,omitempty"`
	Fingerprint SnapshotFingerprint `json:"fingerprint,omitempty"`
	CheckedAt   *time.Time          `json:"checked_at,omitempty"`
	RefreshedAt time.Time           `json:"refreshed_at"`
	Payload     []byte              `json:"payload"`
}

func NewStore(cacheHome string) (*Store, error) {
	cacheHome = strings.TrimSpace(cacheHome)
	if cacheHome == "" {
		return nil, errors.New("cache home must not be empty")
	}
	return &Store{
		root: filepath.Join(cacheHome, cacheDirName, "catalog", cacheVersion),
		now:  time.Now,
	}, nil
}

func ScopeFromSelector(revisionID int64, sha string) Scope {
	sha = strings.TrimSpace(sha)
	switch {
	case revisionID > 0:
		return Scope{
			Key:        fmt.Sprintf("rev:%d", revisionID),
			RevisionID: revisionID,
		}
	case sha != "":
		return Scope{
			Key:      "sha:" + sha,
			SHA:      sha,
			Floating: false,
		}
	default:
		return Scope{
			Key:      "default-branch-latest",
			Floating: true,
		}
	}
}

func (s *Store) LoadRepos(profile string) (Record, bool, error) {
	return s.load(s.recordPath(kindRepos, profile, globalKey, globalKey, globalKey, globalKey))
}

func (s *Store) SaveRepos(profile string, payload []byte) error {
	return s.save(s.recordPath(kindRepos, profile, globalKey, globalKey, globalKey, globalKey), Record{
		Kind:        kindRepos,
		Profile:     profile,
		RefreshedAt: s.now().UTC(),
		Payload:     cloneBytes(payload),
	})
}

func (s *Store) LoadStatus(profile string, repo string) (Record, bool, error) {
	return s.load(s.recordPath(kindStatus, profile, repo, globalKey, globalKey, globalKey))
}

func (s *Store) SaveStatus(profile string, repo string, payload []byte, fingerprint SnapshotFingerprint) error {
	now := s.now().UTC()
	return s.save(s.recordPath(kindStatus, profile, repo, globalKey, globalKey, globalKey), Record{
		Kind:        kindStatus,
		Profile:     profile,
		Repo:        repo,
		Fingerprint: fingerprint,
		CheckedAt:   &now,
		RefreshedAt: now,
		Payload:     cloneBytes(payload),
	})
}

func (s *Store) TouchStatus(profile string, repo string, record Record) error {
	checkedAt := s.now().UTC()
	record.CheckedAt = &checkedAt
	return s.save(s.recordPath(kindStatus, profile, repo, globalKey, globalKey, globalKey), record)
}

func (s *Store) LoadAPIs(profile string, repo string, scope Scope) (Record, bool, error) {
	return s.load(s.recordPath(kindAPIs, profile, repo, globalKey, scope.Key, globalKey))
}

func (s *Store) SaveAPIs(profile string, repo string, scope Scope, payload []byte, fingerprint SnapshotFingerprint) error {
	return s.save(s.recordPath(kindAPIs, profile, repo, globalKey, scope.Key, globalKey), Record{
		Kind:        kindAPIs,
		Profile:     profile,
		Repo:        repo,
		Scope:       scope.Key,
		Fingerprint: fingerprint,
		RefreshedAt: s.now().UTC(),
		Payload:     cloneBytes(payload),
	})
}

func (s *Store) LoadOperations(profile string, repo string, api string, scope Scope) (Record, bool, error) {
	normalizedAPI := api
	if strings.TrimSpace(normalizedAPI) == "" {
		normalizedAPI = globalKey
	}
	return s.load(s.recordPath(kindOperations, profile, repo, normalizedAPI, scope.Key, globalKey))
}

func (s *Store) SaveOperations(profile string, repo string, api string, scope Scope, payload []byte, fingerprint SnapshotFingerprint) error {
	normalizedAPI := api
	if strings.TrimSpace(normalizedAPI) == "" {
		normalizedAPI = globalKey
	}
	return s.save(s.recordPath(kindOperations, profile, repo, normalizedAPI, scope.Key, globalKey), Record{
		Kind:        kindOperations,
		Profile:     profile,
		Repo:        repo,
		API:         strings.TrimSpace(api),
		Scope:       scope.Key,
		Fingerprint: fingerprint,
		RefreshedAt: s.now().UTC(),
		Payload:     cloneBytes(payload),
	})
}

func (s *Store) LoadSpec(profile string, repo string, api string, scope Scope, format string) (Record, bool, error) {
	return s.load(s.recordPath(specKind(format), profile, repo, api, scope.Key, globalKey))
}

func (s *Store) SaveSpec(
	profile string,
	repo string,
	api string,
	scope Scope,
	format string,
	payload []byte,
	fingerprint SnapshotFingerprint,
) error {
	return s.save(s.recordPath(specKind(format), profile, repo, api, scope.Key, globalKey), Record{
		Kind:        specKind(format),
		Profile:     profile,
		Repo:        repo,
		API:         api,
		Scope:       scope.Key,
		Fingerprint: fingerprint,
		RefreshedAt: s.now().UTC(),
		Payload:     cloneBytes(payload),
	})
}

func (s *Store) LoadOperationResponse(profile string, repo string, api string, scope Scope, selectorKey string) (Record, bool, error) {
	return s.load(s.recordPath(kindOperation, profile, repo, api, scope.Key, selectorKey))
}

func (s *Store) SaveOperationResponse(
	profile string,
	repo string,
	api string,
	scope Scope,
	selectorKey string,
	payload []byte,
	fingerprint SnapshotFingerprint,
) error {
	return s.save(s.recordPath(kindOperation, profile, repo, api, scope.Key, selectorKey), Record{
		Kind:        kindOperation,
		Profile:     profile,
		Repo:        repo,
		API:         api,
		Scope:       scope.Key,
		SelectorKey: selectorKey,
		Fingerprint: fingerprint,
		RefreshedAt: s.now().UTC(),
		Payload:     cloneBytes(payload),
	})
}

func (s *Store) LoadCallPlan(profile string, repo string, api string, scope Scope, selectorKey string) (Record, bool, error) {
	return s.load(s.recordPath(kindCallPlan, profile, repo, api, scope.Key, selectorKey))
}

func (s *Store) SaveCallPlan(
	profile string,
	repo string,
	api string,
	scope Scope,
	selectorKey string,
	payload []byte,
	fingerprint SnapshotFingerprint,
) error {
	return s.save(s.recordPath(kindCallPlan, profile, repo, api, scope.Key, selectorKey), Record{
		Kind:        kindCallPlan,
		Profile:     profile,
		Repo:        repo,
		API:         api,
		Scope:       scope.Key,
		SelectorKey: selectorKey,
		Fingerprint: fingerprint,
		RefreshedAt: s.now().UTC(),
		Payload:     cloneBytes(payload),
	})
}

func (s *Store) save(path string, record Record) error {
	if s == nil {
		return errors.New("catalog store is not configured")
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create catalog cache dir: %w", err)
	}

	body, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode catalog cache record: %w", err)
	}

	tempFile, err := os.CreateTemp(directory, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp catalog cache file: %w", err)
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	if _, err := tempFile.Write(body); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("write catalog cache file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close catalog cache file: %w", err)
	}
	if err := os.Chmod(tempPath, 0o644); err != nil {
		return fmt.Errorf("chmod catalog cache file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace catalog cache file: %w", err)
	}
	return nil
}

func (s *Store) load(path string) (Record, bool, error) {
	if s == nil {
		return Record{}, false, errors.New("catalog store is not configured")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Record{}, false, nil
		}
		return Record{}, false, fmt.Errorf("read catalog cache file: %w", err)
	}

	var record Record
	if err := json.Unmarshal(content, &record); err != nil {
		return Record{}, false, fmt.Errorf("decode catalog cache file: %w", err)
	}
	return record, true, nil
}

func (s *Store) recordPath(kind string, profile string, repo string, api string, scope string, selectorKey string) string {
	parts := []string{
		safeSegment(kind),
		safeSegment(profile),
		safeSegment(repo),
		safeSegment(api),
		safeSegment(scope),
		safeSegment(selectorKey) + ".json",
	}
	return filepath.Join(append([]string{s.root}, parts...)...)
}

func specKind(format string) string {
	switch strings.TrimSpace(format) {
	case "yaml":
		return kindSpecYAML
	default:
		return kindSpecJSON
	}
}

func safeSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = globalKey
	}
	sum := sha256.Sum256([]byte(value))
	hash := hex.EncodeToString(sum[:8])

	builder := strings.Builder{}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-', r == '_', r == '.':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
		if builder.Len() >= 32 {
			break
		}
	}

	prefix := strings.Trim(builder.String(), "_")
	if prefix == "" {
		prefix = "key"
	}
	return prefix + "-" + hash
}

func cloneBytes(value []byte) []byte {
	if len(value) == 0 {
		return nil
	}
	return append([]byte(nil), value...)
}
