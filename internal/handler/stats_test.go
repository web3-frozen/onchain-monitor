package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/web3-frozen/onchain-monitor/internal/monitor"
)

func TestStatsHandler(t *testing.T) {
	engine := monitor.NewEngine(nil, slog.Default(), nil, nil)

	// Register a mock source
	engine.Register(&mockSource{
		name:  "testsrc",
		chain: "TestChain",
		snapshot: &monitor.Snapshot{
			Source: "testsrc",
			Chain:  "TestChain",
			Metrics: map[string]float64{
				"tvl": 1000000,
			},
		},
	})

	// Trigger a poll by setting snapshot directly via GetSnapshot won't work
	// since no poll has run. Instead test empty response.
	handler := Stats(engine)

	// All sources (no data yet since no poll)
	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var snaps []*monitor.Snapshot
	if err := json.NewDecoder(rec.Body).Decode(&snaps); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// No poll has run, so empty
	if len(snaps) != 0 {
		t.Errorf("len(snaps) = %d, want 0", len(snaps))
	}

	// Single source not found
	req = httptest.NewRequest(http.MethodGet, "/api/stats?source=nonexistent", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("missing source: status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestStatsMetadataHandler(t *testing.T) {
	engine := monitor.NewEngine(nil, slog.Default(), nil, nil)
	engine.Register(&mockSource{name: "src1", chain: "ChainA"})
	engine.Register(&mockSource{name: "src2", chain: "ChainB"})

	handler := StatsMetadata(engine)
	req := httptest.NewRequest(http.MethodGet, "/api/stats/meta", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var meta struct {
		Chains       []string `json:"chains"`
		PollInterval string   `json:"poll_interval"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&meta); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(meta.Chains) != 2 {
		t.Errorf("len(Chains) = %d, want 2", len(meta.Chains))
	}
	if meta.PollInterval != "60s" {
		t.Errorf("PollInterval = %q, want %q", meta.PollInterval, "60s")
	}
}

// mockSource implements monitor.Source for testing.
type mockSource struct {
	name     string
	chain    string
	snapshot *monitor.Snapshot
}

func (m *mockSource) Name() string  { return m.name }
func (m *mockSource) Chain() string { return m.chain }
func (m *mockSource) URL() string   { return "https://example.com" }

func (m *mockSource) FetchSnapshot() (*monitor.Snapshot, error) {
	if m.snapshot != nil {
		return m.snapshot, nil
	}
	return &monitor.Snapshot{
		Source:  m.name,
		Chain:   m.chain,
		Metrics: map[string]float64{"test": 1},
	}, nil
}

func (m *mockSource) FetchDailyReport() (string, error) {
	return "test report", nil
}
