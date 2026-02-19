package monitor

import (
	"log/slog"
	"sort"
	"testing"
	"time"
)

// mockSource implements Source for testing.
type mockSource struct {
	name  string
	chain string
	snap  *Snapshot
}

func (m *mockSource) Name() string  { return m.name }
func (m *mockSource) Chain() string { return m.chain }
func (m *mockSource) URL() string   { return "https://example.com" }

func (m *mockSource) FetchSnapshot() (*Snapshot, error) {
	if m.snap != nil {
		return m.snap, nil
	}
	return &Snapshot{
		Source:    m.name,
		Chain:     m.chain,
		Metrics:   map[string]float64{"test_metric": 42},
		FetchedAt: time.Now(),
	}, nil
}

func (m *mockSource) FetchDailyReport() (string, error) {
	return "mock daily report for " + m.name, nil
}

func TestEngineRegisterAndSourceNames(t *testing.T) {
	e := NewEngine(nil, slog.Default(), nil, nil)

	e.Register(&mockSource{name: "src1", chain: "Chain1"})
	e.Register(&mockSource{name: "src2", chain: "Chain2"})

	names := e.SourceNames()
	sort.Strings(names)

	if len(names) != 2 {
		t.Fatalf("len(SourceNames) = %d, want 2", len(names))
	}
	if names[0] != "src1" || names[1] != "src2" {
		t.Errorf("SourceNames = %v, want [src1, src2]", names)
	}
}

func TestEngineChains(t *testing.T) {
	e := NewEngine(nil, slog.Default(), nil, nil)
	e.Register(&mockSource{name: "a", chain: "Ethereum"})
	e.Register(&mockSource{name: "b", chain: "Arbitrum"})
	e.Register(&mockSource{name: "c", chain: "Ethereum"}) // duplicate

	chains := e.Chains()
	sort.Strings(chains)

	if len(chains) != 2 {
		t.Fatalf("len(Chains) = %d, want 2", len(chains))
	}
	if chains[0] != "Arbitrum" || chains[1] != "Ethereum" {
		t.Errorf("Chains = %v, want [Arbitrum, Ethereum]", chains)
	}
}

func TestEngineGetSnapshotEmpty(t *testing.T) {
	e := NewEngine(nil, slog.Default(), nil, nil)

	snap := e.GetSnapshot("nonexistent")
	if snap != nil {
		t.Errorf("GetSnapshot(nonexistent) = %v, want nil", snap)
	}
}

func TestFetchWithTimeout(t *testing.T) {
	// Fast source should succeed
	fast := &mockSource{name: "fast", chain: "Test"}
	snap, err := fetchWithTimeout(fast.FetchSnapshot, fetchTimeout)
	if err != nil {
		t.Fatalf("fetchWithTimeout(fast) error: %v", err)
	}
	if snap.Source != "fast" {
		t.Errorf("Source = %q, want %q", snap.Source, "fast")
	}
}

func TestSnapshotGetters(t *testing.T) {
	snap := &Snapshot{
		Metrics: map[string]float64{
			"tvl":   1000000,
			"price": 95000,
			"apr":   12.5,
		},
	}

	if snap.TVL() != 1000000 {
		t.Errorf("TVL() = %v, want 1000000", snap.TVL())
	}
	if snap.Price() != 95000 {
		t.Errorf("Price() = %v, want 95000", snap.Price())
	}
	if snap.APR() != 12.5 {
		t.Errorf("APR() = %v, want 12.5", snap.APR())
	}
}
