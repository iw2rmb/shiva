package catalog

import (
	"sync"
	"testing"
)

func TestStoreSaveAPIsAllowsConcurrentWriters(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	scope := ScopeFromSelector(42, "")
	payload := []byte(`[{"api":"apis/pets/openapi.yaml","has_snapshot":true}]`)
	fingerprint := SnapshotFingerprint{RevisionID: 42}

	const writers = 16
	const iterations = 20

	errCh := make(chan error, writers*iterations)
	var wg sync.WaitGroup
	for writer := 0; writer < writers; writer++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for iteration := 0; iteration < iterations; iteration++ {
				errCh <- store.SaveAPIs("default", "acme/platform", scope, payload, fingerprint)
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent save failed: %v", err)
		}
	}

	record, found, err := store.LoadAPIs("default", "acme/platform", scope)
	if err != nil {
		t.Fatalf("load saved record failed: %v", err)
	}
	if !found {
		t.Fatalf("expected saved record to exist")
	}
	if string(record.Payload) != string(payload) {
		t.Fatalf("unexpected payload %q", string(record.Payload))
	}
}
