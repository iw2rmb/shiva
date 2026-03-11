package store

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestGetStartupIndexLastProjectID(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("boom")
	tests := []struct {
		name          string
		lastProjectID int64
		err           error
		want          int64
		wantErr       error
	}{
		{
			name:          "returns stored checkpoint",
			lastProjectID: 42,
			want:          42,
		},
		{
			name:    "missing checkpoint defaults to zero",
			err:     pgx.ErrNoRows,
			want:    0,
			wantErr: nil,
		},
		{
			name:    "wraps query error",
			err:     expectedErr,
			wantErr: expectedErr,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := getStartupIndexLastProjectID(context.Background(), fakeStartupIndexLastProjectIDQueries{
				lastProjectID: tc.lastProjectID,
				err:           tc.err,
			})
			if tc.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("expected wrapped error %v, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("getStartupIndexLastProjectID() unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("expected last project id %d, got %d", tc.want, got)
			}
		})
	}
}

func TestAdvanceStartupIndexLastProjectID(t *testing.T) {
	t.Parallel()

	fake := &fakeAdvanceStartupIndexLastProjectIDQueries{}
	if err := advanceStartupIndexLastProjectID(context.Background(), fake, 73); err != nil {
		t.Fatalf("advanceStartupIndexLastProjectID() unexpected error: %v", err)
	}
	if fake.lastProjectID != 73 {
		t.Fatalf("expected last project id 73, got %d", fake.lastProjectID)
	}
}

func TestAdvanceStartupIndexLastProjectIDWrapsError(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("boom")
	err := advanceStartupIndexLastProjectID(
		context.Background(),
		&fakeAdvanceStartupIndexLastProjectIDQueries{err: expectedErr},
		73,
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected wrapped error %v, got %v", expectedErr, err)
	}
}

type fakeStartupIndexLastProjectIDQueries struct {
	lastProjectID int64
	err           error
}

func (f fakeStartupIndexLastProjectIDQueries) GetStartupIndexLastProjectID(context.Context) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.lastProjectID, nil
}

type fakeAdvanceStartupIndexLastProjectIDQueries struct {
	lastProjectID int64
	err           error
}

func (f *fakeAdvanceStartupIndexLastProjectIDQueries) AdvanceStartupIndexLastProjectID(
	_ context.Context,
	lastProjectID int64,
) error {
	f.lastProjectID = lastProjectID
	if f.err != nil {
		return f.err
	}
	return nil
}
