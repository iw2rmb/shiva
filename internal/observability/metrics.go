package observability

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

const (
	stageIngest   = "ingest"
	stageBuild    = "build"
	stageDelivery = "delivery"
)

type Metrics struct {
	mu     sync.RWMutex
	stages map[string]stageMetrics
}

type stageMetrics struct {
	SuccessCount      int64   `json:"success_count"`
	FailureCount      int64   `json:"failure_count"`
	SuccessLatencySum float64 `json:"success_latency_sum_seconds"`
	FailureLatencySum float64 `json:"failure_latency_sum_seconds"`
}

type Snapshot struct {
	StageLatencySeconds map[string]stageMetrics `json:"stage_latency_seconds"`
	StageFailuresTotal  map[string]int64        `json:"stage_failures_total"`
}

func NewMetrics() *Metrics {
	return &Metrics{
		stages: map[string]stageMetrics{
			stageIngest:   {},
			stageBuild:    {},
			stageDelivery: {},
		},
	}
}

func (m *Metrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(m.Snapshot())
	})
}

func (m *Metrics) ObserveIngest(duration time.Duration, success bool) {
	m.observeStage(stageIngest, duration, success)
}

func (m *Metrics) ObserveBuild(duration time.Duration, success bool) {
	m.observeStage(stageBuild, duration, success)
}

func (m *Metrics) ObserveDelivery(duration time.Duration, success bool) {
	m.observeStage(stageDelivery, duration, success)
}

func (m *Metrics) Snapshot() Snapshot {
	if m == nil {
		return Snapshot{
			StageLatencySeconds: map[string]stageMetrics{},
			StageFailuresTotal:  map[string]int64{},
		}
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	latency := make(map[string]stageMetrics, len(m.stages))
	failures := make(map[string]int64, len(m.stages))
	for stage, metrics := range m.stages {
		latency[stage] = metrics
		failures[stage] = metrics.FailureCount
	}

	return Snapshot{
		StageLatencySeconds: latency,
		StageFailuresTotal:  failures,
	}
}

func (m *Metrics) observeStage(stage string, duration time.Duration, success bool) {
	if m == nil {
		return
	}

	seconds := duration.Seconds()
	m.mu.Lock()
	defer m.mu.Unlock()

	current := m.stages[stage]
	if success {
		current.SuccessCount++
		current.SuccessLatencySum += seconds
	} else {
		current.FailureCount++
		current.FailureLatencySum += seconds
	}
	m.stages[stage] = current
}
