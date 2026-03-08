package sources

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/web3-frozen/onchain-monitor/internal/monitor"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

const defillamaAPI = "https://yields.llama.fi/pools?include=flexible"

// Pre-compiled regex patterns for withdrawal day parsing
var (
	withdrawalDaysRe = regexp.MustCompile(`(\d+)\s*days?\s*(?:unstaking|lockup|lock|withdrawal)`)
	withdrawalDRe    = regexp.MustCompile(`(\d+)d\s*(?:unstaking|lockup|lock|withdrawal)?`)
	withdrawalWeeksRe = regexp.MustCompile(`(\d+)\s*weeks?\s*(?:unstaking|lockup|lock|withdrawal)`)
)

// DefiLlamaPool represents a single yield pool from DeFi Llama.
type DefiLlamaPool struct {
	Chain           string   `json:"chain"`
	Project         string   `json:"project"`
	Symbol          string   `json:"symbol"`
	TVLUsd          float64  `json:"tvlUsd"`
	APY             float64  `json:"apy"`
	APYBase         *float64 `json:"apyBase"`
	APYReward       *float64 `json:"apyReward"`
	Pool            string   `json:"pool"` // Unique pool ID
	PoolMeta        *string  `json:"poolMeta"`
	Stablecoin      bool     `json:"stablecoin"`
	ILRisk          string   `json:"ilRisk"`
	Exposure        string   `json:"exposure"`
	UnderlyingToken []string `json:"underlyingTokens"`
}

// WithdrawalDays parses the poolMeta field to extract withdrawal delay.
// Returns 0 if immediate withdrawal, or the number of days if there's a lock/unstaking period.
func (p *DefiLlamaPool) WithdrawalDays() int {
	if p.PoolMeta == nil || *p.PoolMeta == "" {
		return 0 // Immediate withdrawal
	}

	meta := strings.ToLower(*p.PoolMeta)

	// Check days pattern: "7 days unstaking", "7 day lockup", etc.
	if matches := withdrawalDaysRe.FindStringSubmatch(meta); len(matches) > 1 {
		if d, err := strconv.Atoi(matches[1]); err == nil {
			return d
		}
	}

	// Check short format: "7d", "7d lockup", etc.
	if matches := withdrawalDRe.FindStringSubmatch(meta); len(matches) > 1 {
		if d, err := strconv.Atoi(matches[1]); err == nil {
			return d
		}
	}

	// Check weeks pattern: "2 weeks unstaking", etc.
	if matches := withdrawalWeeksRe.FindStringSubmatch(meta); len(matches) > 1 {
		if w, err := strconv.Atoi(matches[1]); err == nil {
			return w * 7
		}
	}

	// Check for known lock indicators without specific days
	lockIndicators := []string{"lock", "vesting", "epoch", "unstaking", "cooldown"}
	for _, ind := range lockIndicators {
		if strings.Contains(meta, ind) {
			return 7 // Default to 7 days if lock mentioned but no specific time
		}
	}

	return 0
}

// IsUSDC returns true if the symbol contains USDC.
func (p *DefiLlamaPool) IsUSDC() bool {
	sym := strings.ToUpper(p.Symbol)
	return strings.Contains(sym, "USDC")
}

// IsUSDT returns true if the symbol contains USDT.
func (p *DefiLlamaPool) IsUSDT() bool {
	sym := strings.ToUpper(p.Symbol)
	return strings.Contains(sym, "USDT")
}

// ProjectDisplayName returns a formatted project name.
func (p *DefiLlamaPool) ProjectDisplayName() string {
	// Convert project slug to display name
	name := strings.ReplaceAll(p.Project, "-", " ")
	caser := cases.Title(language.English)
	return caser.String(name)
}

// DefiLlamaURL returns the DeFi Llama yields page URL.
func (p *DefiLlamaPool) DefiLlamaURL() string {
	return "https://defillama.com/yields"
}

type defillamaResponse struct {
	Status string          `json:"status"`
	Data   []DefiLlamaPool `json:"data"`
}

// DefiLlama fetches stablecoin yield opportunities from DeFi Llama.
type DefiLlama struct {
	baseURL string
	client  *http.Client
	logger  *slog.Logger
	mu      sync.RWMutex
	pools   []DefiLlamaPool
}

func NewDefiLlama(logger *slog.Logger) *DefiLlama {
	return &DefiLlama{
		baseURL: defillamaAPI,
		client:  &http.Client{Timeout: 30 * time.Second},
		logger:  logger,
	}
}

func (d *DefiLlama) Name() string  { return "defillama" }
func (d *DefiLlama) Chain() string { return "General" }
func (d *DefiLlama) URL() string   { return "https://defillama.com/yields" }

// FetchAllPools fetches all pools from DeFi Llama API.
func (d *DefiLlama) FetchAllPools() ([]DefiLlamaPool, error) {
	resp, err := d.client.Get(d.baseURL)
	if err != nil {
		return nil, fmt.Errorf("defillama API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("defillama API status: %d", resp.StatusCode)
	}

	var data defillamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode defillama: %w", err)
	}

	if data.Status != "success" {
		return nil, fmt.Errorf("defillama API returned status: %s", data.Status)
	}

	return data.Data, nil
}

// FilterStablePools filters pools for stablecoin yields with given criteria.
// tokenFilter: "USDC", "USDT", "USDC_USDT", "ALL_STABLES"
// maxWithdrawDays: maximum withdrawal days (0 = immediate only, 7 = up to 7 days, etc.)
func (d *DefiLlama) FilterStablePools(pools []DefiLlamaPool, minAPY, minTVL float64, tokenFilter string, maxWithdrawDays int) []DefiLlamaPool {
	var result []DefiLlamaPool

	for _, p := range pools {
		// Filter by APY
		if p.APY < minAPY {
			continue
		}

		// Filter by TVL
		if p.TVLUsd < minTVL {
			continue
		}

		// Filter by withdrawal days
		if p.WithdrawalDays() > maxWithdrawDays {
			continue
		}

		// Filter by token type
		switch tokenFilter {
		case "USDC":
			if !p.IsUSDC() {
				continue
			}
		case "USDT":
			if !p.IsUSDT() {
				continue
			}
		case "USDC_USDT":
			if !p.IsUSDC() && !p.IsUSDT() {
				continue
			}
		case "ALL_STABLES":
			if !p.Stablecoin {
				continue
			}
		default:
			// For any other value, require stablecoin
			if !p.Stablecoin {
				continue
			}
		}

		result = append(result, p)
	}

	// Sort by APY descending
	sort.Slice(result, func(i, j int) bool {
		return result[i].APY > result[j].APY
	})

	return result
}

// GetPools returns the latest cached pools.
func (d *DefiLlama) GetPools() []DefiLlamaPool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]DefiLlamaPool, len(d.pools))
	copy(out, d.pools)
	return out
}

// GetFilteredPools fetches and filters pools for alert matching.
func (d *DefiLlama) GetFilteredPools(minAPY, minTVL float64, tokenFilter string, maxWithdrawDays int) []monitor.DefiLlamaOpp {
	pools, err := d.FetchAllPools()
	if err != nil {
		d.logger.Error("defillama filter API failed", "error", err)
		return nil
	}

	filtered := d.FilterStablePools(pools, minAPY, minTVL, tokenFilter, maxWithdrawDays)

	var result []monitor.DefiLlamaOpp
	for _, p := range filtered {
		result = append(result, monitor.DefiLlamaOpp{
			Pool:           p.Pool,
			Project:        p.ProjectDisplayName(),
			Symbol:         p.Symbol,
			Chain:          p.Chain,
			APY:            p.APY,
			TVLUsd:         p.TVLUsd,
			WithdrawalDays: p.WithdrawalDays(),
			Stablecoin:     p.Stablecoin,
			PoolMeta:       p.PoolMeta,
			URL:            p.DefiLlamaURL(),
		})
	}
	return result
}

func (d *DefiLlama) FetchSnapshot() (*monitor.Snapshot, error) {
	pools, err := d.FetchAllPools()
	if err != nil {
		return nil, err
	}

	// Filter for stablecoins with APY > 0.1%, TVL > 100K, withdrawal <= 7 days
	// This is for dashboard display
	filtered := d.FilterStablePools(pools, 0.1, 100000, "USDC_USDT", 7)

	d.mu.Lock()
	d.pools = filtered
	d.mu.Unlock()

	// Calculate metrics
	var usdcMaxAPY, usdtMaxAPY float64
	var usdcCount, usdtCount int

	for _, p := range filtered {
		if p.IsUSDC() {
			usdcCount++
			if p.APY > usdcMaxAPY {
				usdcMaxAPY = p.APY
			}
		}
		if p.IsUSDT() {
			usdtCount++
			if p.APY > usdtMaxAPY {
				usdtMaxAPY = p.APY
			}
		}
	}

	return &monitor.Snapshot{
		Source: d.Name(),
		Chain:  d.Chain(),
		Metrics: map[string]float64{
			"usdc_max_apy":  usdcMaxAPY,
			"usdt_max_apy":  usdtMaxAPY,
			"usdc_pools":    float64(usdcCount),
			"usdt_pools":    float64(usdtCount),
			"total_pools":   float64(len(filtered)),
		},
		DataSources: map[string]string{
			"usdc_max_apy":  "DeFi Llama",
			"usdt_max_apy":  "DeFi Llama",
			"usdc_pools":    "DeFi Llama",
			"usdt_pools":    "DeFi Llama",
			"total_pools":   "DeFi Llama",
		},
		FetchedAt: time.Now(),
	}, nil
}

func (d *DefiLlama) FetchDailyReport() (string, error) {
	d.mu.RLock()
	pools := d.pools
	d.mu.RUnlock()

	if len(pools) == 0 {
		return "", fmt.Errorf("no defillama data available")
	}

	// Sort by APY descending
	sorted := make([]DefiLlamaPool, len(pools))
	copy(sorted, pools)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].APY > sorted[j].APY
	})

	var b strings.Builder
	b.WriteString("💰 DeFi Llama USDC/USDT Yields Report\n\n")

	count := 10
	if len(sorted) < count {
		count = len(sorted)
	}

	for i := 0; i < count; i++ {
		p := sorted[i]
		withdrawalInfo := "✅ Immediate"
		if days := p.WithdrawalDays(); days > 0 {
			withdrawalInfo = fmt.Sprintf("⏱️ %dd", days)
		}

		b.WriteString(fmt.Sprintf("%d. %s - %s\n", i+1, p.ProjectDisplayName(), p.Symbol))
		b.WriteString(fmt.Sprintf("   Chain: %s | APY: %.2f%% | TVL: $%s\n",
			p.Chain, p.APY, fmtTVL(p.TVLUsd)))
		b.WriteString(fmt.Sprintf("   Withdrawal: %s\n", withdrawalInfo))
		b.WriteString("\n")
	}

	b.WriteString(fmt.Sprintf("Total: %d USDC/USDT pools tracked\n", len(pools)))
	b.WriteString("🔗 https://defillama.com/yields")
	return b.String(), nil
}
