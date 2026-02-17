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
}

// Snapshot represents a point-in-time reading from a data source.
type Snapshot struct {
	Source    string    `json:"source"`
	TVL      float64   `json:"tvl"`
	Price    float64   `json:"price"`
	APR      float64   `json:"apr"`
	FetchedAt time.Time `json:"fetched_at"`
}
