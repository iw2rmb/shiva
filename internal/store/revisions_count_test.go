package store

import (
	"context"
	"errors"
	"testing"
)

func TestCountRevisions(t *testing.T) {
	t.Parallel()

	count, err := countRevisions(context.Background(), fakeRevisionCountQueries{count: 7})
	if err != nil {
		t.Fatalf("countRevisions() unexpected error: %v", err)
	}
	if count != 7 {
		t.Fatalf("expected count 7, got %d", count)
	}
}

func TestCountRevisions_WrapsError(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("boom")
	_, err := countRevisions(context.Background(), fakeRevisionCountQueries{err: expectedErr})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected wrapped error %v, got %v", expectedErr, err)
	}
}

type fakeRevisionCountQueries struct {
	count int64
	err   error
}

func (f fakeRevisionCountQueries) CountRevisions(context.Context) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.count, nil
}
