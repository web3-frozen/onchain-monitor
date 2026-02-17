package monitor

import "time"

// Source defines the interface that all data sources must implement.
// To add a new on-chain data source, create a struct that implements this
// interface and register it with the Engine.
type Source interface {
	// Name returns a unique identifier for this source (e.g., "altura").
	Name() string

	// FetchSnapshot fetches the current state from the data source.
	FetchSnapshot() (*Snapshot, error)

	// FetchDailyReport generates a daily report string.
	FetchDailyReport() (string, error)

	// URL returns the link to the source's stats page.
	URL() string
}

// Snapshot represents a point-in-time reading from a data source.
type Snapshot struct {
	Source    string             `json:"source"`
	Metrics  map[string]float64 `json:"metrics"`
	FetchedAt time.Time         `json:"fetched_at"`
}

// legacy convenience getters used by existing code
func (s *Snapshot) TVL() float64   { return s.Metrics["tvl"] }
func (s *Snapshot) Price() float64 { return s.Metrics["price"] }
func (s *Snapshot) APR() float64   { return s.Metrics["apr"] }
