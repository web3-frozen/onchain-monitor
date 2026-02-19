package sources

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/web3-frozen/onchain-monitor/internal/monitor"
)

const turtleAPI = "https://api.turtle.xyz/turtle/opportunities"

// TurtleOpportunity represents a single yield opportunity from Turtle.
type TurtleOpportunity struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Type      string  `json:"type"`
	TVL       float64 `json:"tvl"`
	TurtleTVL float64 `json:"turtleTvl"`
	Status    string  `json:"status"`

	DepositTokens []struct {
		Symbol string  `json:"symbol"`
		Price  float64 `json:"priceUsd"`
		Chain  struct {
			Name string `json:"name"`
			Slug string `json:"slug"`
		} `json:"chain"`
	} `json:"depositTokens"`

	Products []struct {
		Name         string `json:"name"`
		Organization struct {
			Name string `json:"name"`
		} `json:"organization"`
	} `json:"products"`

	Incentives []struct {
		Name  string  `json:"name"`
		Yield float64 `json:"yield"`
	} `json:"incentives"`

	Tags []struct {
		Code string `json:"code"`
		Name string `json:"name"`
	} `json:"tags"`
}

// TotalYield returns the sum of all incentive yields (APR %).
func (o *TurtleOpportunity) TotalYield() float64 {
	var total float64
	for _, inc := range o.Incentives {
		total += inc.Yield
	}
	return total
}

// ChainName returns the chain name from the first deposit token.
func (o *TurtleOpportunity) ChainName() string {
	if len(o.DepositTokens) > 0 {
		return o.DepositTokens[0].Chain.Name
	}
	return "Unknown"
}

// TokenSymbol returns the token symbol from the first deposit token.
func (o *TurtleOpportunity) TokenSymbol() string {
	if len(o.DepositTokens) > 0 {
		return o.DepositTokens[0].Symbol
	}
	return "?"
}

// OrganizationName returns the first product's organization name.
func (o *TurtleOpportunity) OrganizationName() string {
	if len(o.Products) > 0 && o.Products[0].Organization.Name != "" {
		return o.Products[0].Organization.Name
	}
	return "Unknown"
}

// TurtleURL returns the link to the Turtle earn page.
func (o *TurtleOpportunity) TurtleURL() string {
	return "https://app.turtle.xyz/earn/opportunities"
}

// IsStablecoin returns true if all deposit tokens are stablecoins.
func (o *TurtleOpportunity) IsStablecoin() bool {
	if len(o.DepositTokens) == 0 {
		return false
	}
	for _, t := range o.DepositTokens {
		sym := strings.ToUpper(t.Symbol)
		if stableSymbols[sym] {
			continue
		}
		if t.Price >= 0.95 && t.Price <= 1.05 {
			continue
		}
		return false
	}
	return true
}

var stableSymbols = map[string]bool{
	"USDC": true, "USDT": true, "DAI": true, "FRAX": true, "GHO": true,
	"LUSD": true, "TUSD": true, "BUSD": true, "PYUSD": true, "USDP": true,
	"USDT0": true, "USD₮0": true, "USDE": true, "USP": true, "USDA": true,
	"EUSD": true, "CUSD": true, "CRVUSD": true, "DOLA": true, "SUSD": true,
	"USDS": true, "AUSD": true, "MUSD": true, "USDAI": true, "SUSDE": true,
}

type turtleResponse struct {
	Opportunities []TurtleOpportunity `json:"opportunities"`
}

// Turtle fetches yield opportunities from the Turtle API.
type Turtle struct {
	baseURL string
	client  *http.Client
	logger  *slog.Logger
	mu      sync.RWMutex
	opps    []TurtleOpportunity
}

func NewTurtle(logger *slog.Logger) *Turtle {
	return &Turtle{
		baseURL: turtleAPI,
		client:  &http.Client{Timeout: 30 * time.Second},
		logger:  logger,
	}
}

func (t *Turtle) Name() string  { return "turtle" }
func (t *Turtle) Chain() string { return "General" }
func (t *Turtle) URL() string   { return "https://app.turtle.xyz/earn/opportunities" }

// FetchAllOpportunities fetches all opportunities from Turtle API.
func (t *Turtle) FetchAllOpportunities() ([]TurtleOpportunity, error) {
	resp, err := t.client.Get(t.baseURL)
	if err != nil {
		return nil, fmt.Errorf("turtle API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("turtle API status: %d", resp.StatusCode)
	}

	var data turtleResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode turtle: %w", err)
	}
	return data.Opportunities, nil
}

// filterOpportunities applies TVL, yield, status filters client-side.
func filterOpportunities(opps []TurtleOpportunity, minYield, minTVL float64) []TurtleOpportunity {
	var result []TurtleOpportunity
	for _, o := range opps {
		if o.Status != "active" {
			continue
		}
		if o.TVL < minTVL {
			continue
		}
		if o.TotalYield() < minYield {
			continue
		}
		result = append(result, o)
	}
	return result
}

// GetOpportunities returns the latest cached opportunities.
func (t *Turtle) GetOpportunities() []TurtleOpportunity {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]TurtleOpportunity, len(t.opps))
	copy(out, t.opps)
	return out
}

// GetFilteredOpportunities fetches and filters opportunities for alert matching.
func (t *Turtle) GetFilteredOpportunities(minAPR, minTVL float64, action, stableFilter string) []monitor.TurtleOpp {
	opps, err := t.FetchAllOpportunities()
	if err != nil {
		t.logger.Error("turtle filter API failed", "error", err)
		return nil
	}

	filtered := filterOpportunities(opps, minAPR, minTVL)

	var result []monitor.TurtleOpp
	for _, o := range filtered {
		// Apply stablecoin filter
		switch stableFilter {
		case "stablecoin":
			if !o.IsStablecoin() {
				continue
			}
		case "non-stablecoin":
			if o.IsStablecoin() {
				continue
			}
		}

		// Apply type/action filter
		if action != "" && action != "ALL" {
			matched := false
			for _, a := range strings.Split(action, ",") {
				if strings.EqualFold(o.Type, strings.TrimSpace(a)) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}

		result = append(result, monitor.TurtleOpp{
			ID:           o.ID,
			Name:         o.Name,
			Type:         o.Type,
			TVL:          o.TVL,
			APR:          o.TotalYield(),
			ChainName:    o.ChainName(),
			Organization: o.OrganizationName(),
			Token:        o.TokenSymbol(),
			TurtleURL:    o.TurtleURL(),
			Stablecoin:   o.IsStablecoin(),
		})
	}
	return result
}

func (t *Turtle) FetchSnapshot() (*monitor.Snapshot, error) {
	opps, err := t.FetchAllOpportunities()
	if err != nil {
		return nil, err
	}

	// Filter for dashboard: active, TVL >= 500K, yield >= 5%
	filtered := filterOpportunities(opps, 5, 500000)

	t.mu.Lock()
	t.opps = filtered
	t.mu.Unlock()

	var topYield float64
	for _, o := range filtered {
		if y := o.TotalYield(); y > topYield {
			topYield = y
		}
	}

	return &monitor.Snapshot{
		Source: t.Name(),
		Chain:  t.Chain(),
		Metrics: map[string]float64{
			"opportunities": float64(len(filtered)),
			"top_yield":     topYield,
		},
		DataSources: map[string]string{
			"opportunities": "Turtle",
			"top_yield":     "Turtle",
		},
		FetchedAt: time.Now(),
	}, nil
}

func (t *Turtle) FetchDailyReport() (string, error) {
	t.mu.RLock()
	opps := t.opps
	t.mu.RUnlock()

	if len(opps) == 0 {
		return "", fmt.Errorf("no turtle data available")
	}

	// Sort by yield descending
	sorted := make([]TurtleOpportunity, len(opps))
	copy(sorted, opps)
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].TotalYield() > sorted[i].TotalYield() {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	var b strings.Builder
	b.WriteString("🐢 Turtle Yield Opportunities Report\n\n")
	count := 5
	if len(sorted) < count {
		count = len(sorted)
	}
	for i := 0; i < count; i++ {
		o := sorted[i]
		stable := ""
		if o.IsStablecoin() {
			stable = " 🟢"
		}
		b.WriteString(fmt.Sprintf("%d. %s%s\n", i+1, o.Name, stable))
		b.WriteString(fmt.Sprintf("   Yield: %.1f%% | TVL: $%s | %s | %s\n",
			o.TotalYield(), fmtTVL(o.TVL), o.ChainName(), o.Type))
		b.WriteString(fmt.Sprintf("   Protocol: %s | Token: %s\n", o.OrganizationName(), o.TokenSymbol()))
		b.WriteString("\n")
	}
	b.WriteString(fmt.Sprintf("Total: %d opportunities\n", len(opps)))
	b.WriteString("🔗 https://app.turtle.xyz/earn/opportunities")
	return b.String(), nil
}
