package sources

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/web3-frozen/onchain-monitor/internal/monitor"
)

const (
	defillamaTVLProtocolsAPI = "https://api.llama.fi/protocols"
	defillamaTVLProtocolAPI  = "https://api.llama.fi/protocol/"
)

// DefiLlamaProtocol represents a protocol from the DeFi Llama protocols API.
type DefiLlamaProtocol struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Slug      string   `json:"slug"`
	TVL       float64  `json:"tvl"`
	Change1d  *float64 `json:"change_1d"`
	Change7d  *float64 `json:"change_7d"`
	Logo      string   `json:"logo"`
	Category  string   `json:"category"`
	Chains    []string `json:"chains"`
	Chain     string   `json:"chain"`
	URL       string   `json:"url"`
}

// defillamaProtocolDetail represents the response from /protocol/{slug}.
type defillamaProtocolDetail struct {
	TVL []struct {
		Date     int64   `json:"date"`
		TotalLiq float64 `json:"totalLiquidityUSD"`
	} `json:"tvl"`
}

// DefiLlamaTVL fetches protocol TVL data from DeFi Llama for TVL change alerts.
type DefiLlamaTVL struct {
	client    *http.Client
	logger    *slog.Logger
	mu        sync.RWMutex
	protocols []DefiLlamaProtocol
	// Cache for 30d TVL change per protocol slug
	tvl30dCache   map[string]float64
	tvl30dCacheAt time.Time
}

func NewDefiLlamaTVL(logger *slog.Logger) *DefiLlamaTVL {
	return &DefiLlamaTVL{
		client:      &http.Client{Timeout: 30 * time.Second},
		logger:      logger,
		tvl30dCache: make(map[string]float64),
	}
}

func (d *DefiLlamaTVL) Name() string  { return "defillama_tvl" }
func (d *DefiLlamaTVL) Chain() string { return "General" }
func (d *DefiLlamaTVL) URL() string   { return "https://defillama.com" }

func (d *DefiLlamaTVL) FetchSnapshot() (*monitor.Snapshot, error) {
	protocols, err := d.fetchProtocols()
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	d.protocols = protocols
	d.mu.Unlock()

	// Calculate aggregate metrics for the dashboard
	var totalTVL float64
	for _, p := range protocols {
		if p.TVL > 0 {
			totalTVL += p.TVL
		}
	}

	return &monitor.Snapshot{
		Source: d.Name(),
		Chain:  d.Chain(),
		Metrics: map[string]float64{
			"total_protocols": float64(len(protocols)),
			"total_tvl":      totalTVL,
		},
		DataSources: map[string]string{
			"total_protocols": "DeFi Llama",
			"total_tvl":      "DeFi Llama",
		},
		FetchedAt: time.Now(),
	}, nil
}

func (d *DefiLlamaTVL) FetchDailyReport() (string, error) {
	return "", fmt.Errorf("daily report not supported for defillama_tvl")
}

func (d *DefiLlamaTVL) fetchProtocols() ([]DefiLlamaProtocol, error) {
	resp, err := d.client.Get(defillamaTVLProtocolsAPI)
	if err != nil {
		return nil, fmt.Errorf("defillama protocols API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("defillama protocols API status: %d", resp.StatusCode)
	}

	var protocols []DefiLlamaProtocol
	if err := json.NewDecoder(resp.Body).Decode(&protocols); err != nil {
		return nil, fmt.Errorf("decode defillama protocols: %w", err)
	}

	return protocols, nil
}

// GetProtocols returns the latest cached protocols.
func (d *DefiLlamaTVL) GetProtocols() []DefiLlamaProtocol {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]DefiLlamaProtocol, len(d.protocols))
	copy(out, d.protocols)
	return out
}

// SearchProtocols returns protocols matching the query string (case-insensitive prefix/substring match).
// Returns at most limit results, sorted by TVL descending.
func (d *DefiLlamaTVL) SearchProtocols(query string, limit int) []DefiLlamaProtocol {
	d.mu.RLock()
	protocols := d.protocols
	d.mu.RUnlock()

	if query == "" || len(protocols) == 0 {
		return nil
	}

	q := strings.ToLower(query)
	var matches []DefiLlamaProtocol

	for _, p := range protocols {
		if p.TVL <= 0 {
			continue
		}
		name := strings.ToLower(p.Name)
		slug := strings.ToLower(p.Slug)
		if strings.Contains(name, q) || strings.Contains(slug, q) {
			matches = append(matches, p)
		}
	}

	// Already sorted by TVL descending from the API, but limit results
	if len(matches) > limit {
		matches = matches[:limit]
	}

	return matches
}

// GetProtocolBySlug returns a protocol by its slug, or nil if not found.
func (d *DefiLlamaTVL) GetProtocolBySlug(slug string) *DefiLlamaProtocol {
	d.mu.RLock()
	protocols := d.protocols
	d.mu.RUnlock()

	slugLower := strings.ToLower(slug)
	for _, p := range protocols {
		if strings.ToLower(p.Slug) == slugLower {
			return &p
		}
	}
	return nil
}

// GetTVLChangePct returns the TVL change percentage for a protocol over the given period.
// periodMinutes: 1440 (1d), 10080 (7d), 43200 (30d)
// Returns the change as a positive percentage (e.g., 5.0 means 5%).
// Negative values mean TVL decreased.
func (d *DefiLlamaTVL) GetTVLChangePct(slug string, periodMinutes int) (float64, error) {
	protocol := d.GetProtocolBySlug(slug)
	if protocol == nil {
		return 0, fmt.Errorf("protocol not found: %s", slug)
	}

	switch periodMinutes {
	case 1440: // 1d
		if protocol.Change1d != nil {
			return *protocol.Change1d, nil
		}
		return 0, fmt.Errorf("1d change not available for %s", slug)

	case 10080: // 7d
		if protocol.Change7d != nil {
			return *protocol.Change7d, nil
		}
		return 0, fmt.Errorf("7d change not available for %s", slug)

	case 43200: // 30d
		return d.fetch30dChange(slug, protocol.TVL)

	default:
		return 0, fmt.Errorf("unsupported period: %d minutes", periodMinutes)
	}
}

// fetch30dChange fetches protocol history to calculate 30d TVL change.
// Uses a cache to avoid excessive API calls (refreshed every 10 minutes).
func (d *DefiLlamaTVL) fetch30dChange(slug string, currentTVL float64) (float64, error) {
	d.mu.RLock()
	cached, hasCached := d.tvl30dCache[slug]
	cacheAge := time.Since(d.tvl30dCacheAt)
	d.mu.RUnlock()

	// Use cache if less than 10 minutes old
	if hasCached && cacheAge < 10*time.Minute {
		return cached, nil
	}

	resp, err := d.client.Get(defillamaTVLProtocolAPI + slug)
	if err != nil {
		if hasCached {
			return cached, nil // Use stale cache on error
		}
		return 0, fmt.Errorf("fetch protocol %s history: %w", slug, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if hasCached {
			return cached, nil
		}
		return 0, fmt.Errorf("protocol %s history API status: %d", slug, resp.StatusCode)
	}

	var detail defillamaProtocolDetail
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		if hasCached {
			return cached, nil
		}
		return 0, fmt.Errorf("decode protocol %s history: %w", slug, err)
	}

	if len(detail.TVL) == 0 {
		return 0, fmt.Errorf("no TVL history for %s", slug)
	}

	// Find TVL 30 days ago (closest entry)
	target := time.Now().Add(-30 * 24 * time.Hour).Unix()
	var closest30dTVL float64
	minDiff := int64(math.MaxInt64)

	for _, entry := range detail.TVL {
		diff := entry.Date - target
		if diff < 0 {
			diff = -diff
		}
		if diff < minDiff {
			minDiff = diff
			closest30dTVL = entry.TotalLiq
		}
	}

	if closest30dTVL <= 0 {
		return 0, fmt.Errorf("no valid 30d TVL data for %s", slug)
	}

	changePct := ((currentTVL - closest30dTVL) / closest30dTVL) * 100

	d.mu.Lock()
	d.tvl30dCache[slug] = changePct
	d.tvl30dCacheAt = time.Now()
	d.mu.Unlock()

	return changePct, nil
}
