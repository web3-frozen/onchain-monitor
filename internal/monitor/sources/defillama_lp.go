package sources

import (
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/web3-frozen/onchain-monitor/internal/monitor"
)

// DefiLlamaLP monitors LP/DEX reward yields from DeFi Llama across user-selected chains.
// It uses the same API as DefiLlama stablecoin source but filters for multi-asset LP pools
// and sorts by reward APY.
type DefiLlamaLP struct {
	baseURL string
	client  *http.Client
	logger  *slog.Logger
	mu      sync.RWMutex
	pools   []DefiLlamaPool
	chains  []string // cached list of available chains
}

func NewDefiLlamaLP(logger *slog.Logger) *DefiLlamaLP {
	return &DefiLlamaLP{
		baseURL: defillamaAPI,
		client:  &http.Client{Timeout: 30 * time.Second},
		logger:  logger,
	}
}

func (d *DefiLlamaLP) Name() string  { return "defillama_lp" }
func (d *DefiLlamaLP) Chain() string { return "General" }
func (d *DefiLlamaLP) URL() string   { return "https://defillama.com/yields?category=Dexs" }

// isLPPool returns true if the pool is a multi-asset LP/DEX pool.
func isLPPool(p DefiLlamaPool) bool {
	if p.Exposure == "multi" {
		return true
	}
	// Fallback: symbol contains "-" (e.g., "WETH-USDC", "SUI-USDC")
	return strings.Contains(p.Symbol, "-")
}

// FilterLPPools filters pools for LP/DEX yields with the given criteria.
// chainFilter: chain name (e.g., "Sui", "Ethereum") or "ALL" for all chains.
// minRewardAPY: minimum reward APY (apyReward field).
// minTVL: minimum TVL in USD.
func (d *DefiLlamaLP) FilterLPPools(pools []DefiLlamaPool, minRewardAPY, minTVL float64, chainFilter string) []DefiLlamaPool {
	var result []DefiLlamaPool

	for _, p := range pools {
		// Must be an LP/DEX pool
		if !isLPPool(p) {
			continue
		}

		// Must have reward APY
		if p.APYReward == nil || *p.APYReward < minRewardAPY {
			continue
		}

		// Filter by TVL
		if p.TVLUsd < minTVL {
			continue
		}

		// Exclude statistical outliers
		if p.Outlier {
			continue
		}

		// Filter by chain
		if chainFilter != "" && chainFilter != "ALL" {
			if !strings.EqualFold(p.Chain, chainFilter) {
				continue
			}
		}

		result = append(result, p)
	}

	// Sort by reward APY descending
	sort.Slice(result, func(i, j int) bool {
		ri, rj := 0.0, 0.0
		if result[i].APYReward != nil {
			ri = *result[i].APYReward
		}
		if result[j].APYReward != nil {
			rj = *result[j].APYReward
		}
		return ri > rj
	})

	return result
}

// GetFilteredLPPools fetches and filters LP pools for alert matching.
func (d *DefiLlamaLP) GetFilteredLPPools(minRewardAPY, minTVL float64, chainFilter string) []monitor.DefiLlamaLPOpp {
	pools, err := d.fetchAllPools()
	if err != nil {
		d.logger.Error("defillama LP filter API failed", "error", err)
		return nil
	}

	filtered := d.FilterLPPools(pools, minRewardAPY, minTVL, chainFilter)

	var result []monitor.DefiLlamaLPOpp
	for _, p := range filtered {
		result = append(result, poolToLPOpp(p))
	}
	return result
}

// GetAvailableChains returns the cached list of chains that have LP pools.
func (d *DefiLlamaLP) GetAvailableChains() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]string, len(d.chains))
	copy(out, d.chains)
	return out
}

func (d *DefiLlamaLP) FetchSnapshot() (*monitor.Snapshot, error) {
	pools, err := d.fetchAllPools()
	if err != nil {
		return nil, err
	}

	// Filter all LP pools with basic thresholds for dashboard display
	lpPools := d.FilterLPPools(pools, 0.1, 100_000, "ALL")

	// Extract unique chains and compute per-chain top reward APY
	chainSet := make(map[string]bool)
	var topRewardAPY float64
	for _, p := range lpPools {
		chainSet[p.Chain] = true
		if p.APYReward != nil && *p.APYReward > topRewardAPY {
			topRewardAPY = *p.APYReward
		}
	}

	chains := make([]string, 0, len(chainSet))
	for c := range chainSet {
		chains = append(chains, c)
	}
	sort.Strings(chains)

	d.mu.Lock()
	d.pools = lpPools
	d.chains = chains
	d.mu.Unlock()

	return &monitor.Snapshot{
		Source: d.Name(),
		Chain:  d.Chain(),
		Metrics: map[string]float64{
			"lp_pools":       float64(len(lpPools)),
			"lp_chains":      float64(len(chains)),
			"lp_top_reward":  topRewardAPY,
		},
		DataSources: map[string]string{
			"lp_pools":      "DeFi Llama",
			"lp_chains":     "DeFi Llama",
			"lp_top_reward": "DeFi Llama",
		},
		FetchedAt: time.Now(),
	}, nil
}

func (d *DefiLlamaLP) FetchDailyReport() (string, error) {
	d.mu.RLock()
	pools := d.pools
	d.mu.RUnlock()

	if len(pools) == 0 {
		return "", fmt.Errorf("no defillama LP data available")
	}

	// Already sorted by reward APY in FilterLPPools
	var b strings.Builder
	b.WriteString("🏊 DeFi Llama LP Rewards Report\n\n")

	count := 15
	if len(pools) < count {
		count = len(pools)
	}

	for i := 0; i < count; i++ {
		p := pools[i]
		rewardAPY := 0.0
		baseAPY := 0.0
		if p.APYReward != nil {
			rewardAPY = *p.APYReward
		}
		if p.APYBase != nil {
			baseAPY = *p.APYBase
		}

		b.WriteString(fmt.Sprintf("%d. %s - %s\n", i+1, p.ProjectDisplayName(), p.Symbol))
		b.WriteString(fmt.Sprintf("   Chain: %s | Reward: %.2f%% + Base: %.2f%% = %.2f%%\n",
			p.Chain, rewardAPY, baseAPY, p.APY))
		b.WriteString(fmt.Sprintf("   TVL: $%s\n", fmtTVL(p.TVLUsd)))
		b.WriteString(fmt.Sprintf("   🔗 %s\n", p.DefiLlamaURL()))
		b.WriteString("\n")
	}

	b.WriteString(fmt.Sprintf("Total: %d LP pools tracked (reward APY ≥ 0.1%%, TVL ≥ $100K)\n", len(pools)))
	return b.String(), nil
}

// fetchAllPools wraps the shared DefiLlama API call.
func (d *DefiLlamaLP) fetchAllPools() ([]DefiLlamaPool, error) {
	// Reuse the same API fetching logic as the stablecoin source.
	tmp := &DefiLlama{baseURL: d.baseURL, client: d.client, logger: d.logger}
	return tmp.FetchAllPools()
}

func poolToLPOpp(p DefiLlamaPool) monitor.DefiLlamaLPOpp {
	return monitor.DefiLlamaLPOpp{
		Pool:      p.Pool,
		Project:   p.ProjectDisplayName(),
		Symbol:    p.Symbol,
		Chain:     p.Chain,
		APY:       p.APY,
		APYBase:   p.APYBase,
		APYReward: p.APYReward,
		TVLUsd:    p.TVLUsd,
		URL:       p.DefiLlamaURL(),
	}
}
