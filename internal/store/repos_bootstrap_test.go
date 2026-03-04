package store

import (
	"context"
	"testing"

	"github.com/iw2rmb/shiva/internal/store/sqlc"
)

func TestGetRepoBootstrapState_ForceRescanAndActiveCount(t *testing.T) {
	t.Parallel()

	queries := &fakeRepoBootstrapQueries{
		state: sqlc.GetRepoBootstrapStateRow{
			ActiveApiCount: 7,
			ForceRescan:    true,
		},
	}

	state, err := getRepoBootstrapState(context.Background(), queries, 12)
	if err != nil {
		t.Fatalf("getRepoBootstrapState() unexpected error: %v", err)
	}

	if state.ActiveAPICount != 7 {
		t.Fatalf("expected active API count=7, got %d", state.ActiveAPICount)
	}
	if !state.ForceRescan {
		t.Fatal("expected force_rescan=true")
	}
}

func TestClearRepoForceRescan_SetsFlagFalse(t *testing.T) {
	t.Parallel()

	queries := &fakeRepoBootstrapQueries{
		state: sqlc.GetRepoBootstrapStateRow{
			ActiveApiCount: 3,
			ForceRescan:    true,
		},
	}

	before, err := getRepoBootstrapState(context.Background(), queries, 12)
	if err != nil {
		t.Fatalf("getRepoBootstrapState() unexpected error: %v", err)
	}
	if !before.ForceRescan {
		t.Fatal("expected force_rescan=true before clear")
	}

	if err := clearRepoForceRescan(context.Background(), queries, 12); err != nil {
		t.Fatalf("clearRepoForceRescan() unexpected error: %v", err)
	}

	after, err := getRepoBootstrapState(context.Background(), queries, 12)
	if err != nil {
		t.Fatalf("getRepoBootstrapState() unexpected error after clear: %v", err)
	}
	if after.ForceRescan {
		t.Fatal("expected force_rescan=false after clear")
	}
}

type fakeRepoBootstrapQueries struct {
	state      sqlc.GetRepoBootstrapStateRow
	lastRepoID int64
}

func (f *fakeRepoBootstrapQueries) GetRepoBootstrapState(_ context.Context, repoID int64) (sqlc.GetRepoBootstrapStateRow, error) {
	f.lastRepoID = repoID
	return f.state, nil
}

func (f *fakeRepoBootstrapQueries) ClearRepoForceRescan(_ context.Context, repoID int64) error {
	f.lastRepoID = repoID
	f.state.ForceRescan = false
	return nil
}
