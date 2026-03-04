package observability

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMetrics_ObserveStagesAndExposeSnapshot(t *testing.T) {
	t.Parallel()

	metrics := NewMetrics()
	metrics.ObserveIngest(125*time.Millisecond, true)
	metrics.ObserveBuild(250*time.Millisecond, false)
	metrics.ObserveDelivery(50*time.Millisecond, false)

	req := httptest.NewRequest(http.MethodGet, "/internal/metrics", nil)
	recorder := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	var snapshot Snapshot
	if err := json.NewDecoder(recorder.Body).Decode(&snapshot); err != nil {
		t.Fatalf("decode metrics snapshot: %v", err)
	}

	if snapshot.StageLatencySeconds["ingest"].SuccessCount != 1 {
		t.Fatalf("expected ingest success_count=1")
	}
	if snapshot.StageLatencySeconds["build"].FailureCount != 1 {
		t.Fatalf("expected build failure_count=1")
	}
	if snapshot.StageLatencySeconds["delivery"].FailureCount != 1 {
		t.Fatalf("expected delivery failure_count=1")
	}
	if snapshot.StageFailuresTotal["build"] != 1 {
		t.Fatalf("expected build failures total=1")
	}
	if snapshot.StageFailuresTotal["delivery"] != 1 {
		t.Fatalf("expected delivery failures total=1")
	}
}
