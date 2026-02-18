package sources

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/web3-frozen/onchain-monitor/internal/monitor"
)

const merklAPI = "https://api.merkl.xyz/v4/opportunities"

// MerklOpportunity represents a single yield opportunity from Merkl.
type MerklOpportunity struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Type       string  `json:"type"`
	Action     string  `json:"action"`
	TVL        float64 `json:"tvl"`
	APR        float64 `json:"apr"`
	Status     string  `json:"status"`
	Identifier string  `json:"identifier"`
	DepositURL string  `json:"depositUrl"`
	Chain      struct {
		Name string `json:"name"`
	} `json:"chain"`
	Protocol *struct {
		Name string `json:"name"`
	} `json:"protocol"`
	Tokens []struct {
		Symbol string  `json:"symbol"`
		Price  float64 `json:"price"`
	} `json:"tokens"`
}

// MerklURL returns the direct link to this opportunity on app.merkl.xyz.
func (o *MerklOpportunity) MerklURL() string {
	chain := strings.ToLower(strings.ReplaceAll(o.Chain.Name, " ", "-"))
	return fmt.Sprintf("https://app.merkl.xyz/opportunities/%s/%s/%s", chain, o.Type, o.Identifier)
}

// IsStablecoin returns true if all tokens in the opportunity are stablecoins.
func (o *MerklOpportunity) IsStablecoin() bool {
	stableSymbols := map[string]bool{
		"USDC": true, "USDT": true, "DAI": true, "FRAX": true, "GHO": true,
		"LUSD": true, "TUSD": true, "BUSD": true, "PYUSD": true, "USDP": true,
		"USDT0": true, "USD₮0": true, "USDE": true, "USP": true, "USDA": true,
		"EUSD": true, "CUSD": true, "CRVUSD": true, "DOLA": true, "SUSD": true,
		"USDS": true, "AUSD": true, "MUSD": true,
	}
	if len(o.Tokens) == 0 {
		return false
	}
	for _, t := range o.Tokens {
		sym := strings.ToUpper(t.Symbol)
		if stableSymbols[sym] {
			continue
		}
		// Price-based fallback: token price near $1
		if t.Price >= 0.95 && t.Price <= 1.05 {
			continue
		}
		return false
	}
	return true
}

// ProtocolName returns the protocol name or "Unknown".
func (o *MerklOpportunity) ProtocolName() string {
	if o.Protocol != nil && o.Protocol.Name != "" {
		return o.Protocol.Name
	}
	return "Unknown"
}

// Merkl fetches yield opportunities from the Merkl API.
type Merkl struct {
	client *http.Client
	mu     sync.RWMutex
	opps   []MerklOpportunity // latest fetched opportunities
}

func NewMerkl() *Merkl {
	return &Merkl{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (m *Merkl) Name() string  { return "merkl" }
func (m *Merkl) Chain() string { return "General" }
func (m *Merkl) URL() string   { return "https://app.merkl.xyz/" }

// GetOpportunities returns the latest cached opportunities.
func (m *Merkl) GetOpportunities() []MerklOpportunity {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]MerklOpportunity, len(m.opps))
	copy(out, m.opps)
	return out
}

// GetFilteredOpportunities returns cached opportunities filtered by user criteria.
func (m *Merkl) GetFilteredOpportunities(minAPR, minTVL float64, action, stableFilter string) []monitor.MerklOpp {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []monitor.MerklOpp
	for _, o := range m.opps {
		if o.APR < minAPR || o.TVL < minTVL {
			continue
		}
		if action != "ALL" && !strings.Contains(action, o.Action) {
			continue
		}
		isStable := o.IsStablecoin()
		if stableFilter == "stablecoin" && !isStable {
			continue
		}
		if stableFilter == "non-stablecoin" && isStable {
			continue
		}
		result = append(result, monitor.MerklOpp{
			ID:         o.ID,
			Name:       o.Name,
			Action:     o.Action,
			TVL:        o.TVL,
			APR:        o.APR,
			ChainName:  o.Chain.Name,
			Protocol:   o.ProtocolName(),
			DepositURL: o.DepositURL,
			MerklURL:   o.MerklURL(),
			Stablecoin: isStable,
		})
	}
	return result
}

// FetchOpportunities fetches opportunities from Merkl with given filters.
func (m *Merkl) FetchOpportunities(minAPR, minTVL float64, action string) ([]MerklOpportunity, error) {
	url := fmt.Sprintf("%s?action=%s&minimumApr=%.0f&minimumTvl=%.0f&sort=apr&order=desc&items=50&status=LIVE",
		merklAPI, action, minAPR, minTVL)

	resp, err := m.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("merkl API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("merkl API status: %d", resp.StatusCode)
	}

	var opps []MerklOpportunity
	if err := json.NewDecoder(resp.Body).Decode(&opps); err != nil {
		return nil, fmt.Errorf("decode merkl: %w", err)
	}
	return opps, nil
}

func (m *Merkl) FetchSnapshot() (*monitor.Snapshot, error) {
	// Fetch top opportunities with broad criteria for dashboard display
	opps, err := m.FetchOpportunities(5, 500000, "LEND,BORROW,HOLD")
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.opps = opps
	m.mu.Unlock()

	var topAPR float64
	for _, o := range opps {
		if o.APR > topAPR {
			topAPR = o.APR
		}
	}

	return &monitor.Snapshot{
		Source: m.Name(),
		Chain:  m.Chain(),
		Metrics: map[string]float64{
			"opportunities": float64(len(opps)),
			"top_apr":       topAPR,
		},
		DataSources: map[string]string{
			"opportunities": "Merkl",
			"top_apr":       "Merkl",
		},
		FetchedAt: time.Now(),
	}, nil
}

func (m *Merkl) FetchDailyReport() (string, error) {
	m.mu.RLock()
	opps := m.opps
	m.mu.RUnlock()

	if len(opps) == 0 {
		return "", fmt.Errorf("no merkl data available")
	}

	var b strings.Builder
	b.WriteString("📊 Merkl Yield Opportunities Report\n\n")
	count := 5
	if len(opps) < count {
		count = len(opps)
	}
	for i := 0; i < count; i++ {
		o := opps[i]
		stable := ""
		if o.IsStablecoin() {
			stable = " 🟢"
		}
		b.WriteString(fmt.Sprintf("%d. %s%s\n", i+1, o.Name, stable))
		b.WriteString(fmt.Sprintf("   APR: %.1f%% | TVL: $%s | %s | %s\n",
			o.APR, fmtTVL(o.TVL), o.Chain.Name, o.Action))
		if o.DepositURL != "" {
			b.WriteString(fmt.Sprintf("   🔗 %s\n", o.DepositURL))
		}
		b.WriteString("\n")
	}
	b.WriteString(fmt.Sprintf("Total: %d opportunities\n", len(opps)))
	b.WriteString("🔗 https://app.merkl.xyz/")
	return b.String(), nil
}

func fmtTVL(v float64) string {
	if v >= 1_000_000 {
		return fmt.Sprintf("%.1fM", v/1_000_000)
	}
	if v >= 1_000 {
		return fmt.Sprintf("%.0fK", v/1_000)
	}
	return fmt.Sprintf("%.0f", v)
}
