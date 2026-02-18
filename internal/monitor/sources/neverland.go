package sources

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/web3-frozen/onchain-monitor/internal/monitor"
)

const (
	neverlandTVLAPI  = "https://api.llama.fi/tvl/neverland"
	neverlandDataAPI = "https://api.llama.fi/protocol/neverland"
	neverlandFeesAPI = "https://api.llama.fi/summary/fees/neverland"
	dustPriceAPI     = "https://api.dexscreener.com/latest/dex/search?q=DUST%20monad"
)

type Neverland struct {
	client *http.Client
}

func NewNeverland() *Neverland {
	return &Neverland{
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (n *Neverland) Name() string  { return "neverland" }
func (n *Neverland) Chain() string { return "Monad" }
func (n *Neverland) URL() string   { return "https://app.neverland.money" }

func (n *Neverland) FetchSnapshot() (*monitor.Snapshot, error) {
	metrics := make(map[string]float64)

	// TVL + staking (veDUST)
	proto, err := n.fetchProtocol()
	if err == nil {
		metrics["tvl"] = proto.CurrentChainTvls.Monad
		metrics["vedust_tvl"] = proto.CurrentChainTvls.Staking
	} else {
		return nil, fmt.Errorf("fetch protocol: %w", err)
	}

	// Fees
	fees, err := n.fetchFees()
	if err == nil {
		metrics["fees_24h"] = fees.Total24h
		metrics["fees_7d"] = fees.Total7d
		metrics["fees_30d"] = fees.Total30d
	}

	// DUST price
	price, err := n.fetchDustPrice()
	if err == nil {
		metrics["price"] = price
	}

	dataSources := map[string]string{
		"tvl":        "DefiLlama",
		"vedust_tvl": "DefiLlama",
		"fees_24h":   "DefiLlama",
		"fees_7d":    "DefiLlama",
		"fees_30d":   "DefiLlama",
		"price":      "DexScreener",
	}

	return &monitor.Snapshot{
		Source:      "neverland",
		Chain:       "Monad",
		Metrics:     metrics,
		DataSources: dataSources,
		FetchedAt:   time.Now(),
	}, nil
}

func (n *Neverland) FetchDailyReport() (string, error) {
	snap, err := n.FetchSnapshot()
	if err != nil {
		return "", err
	}

	now := time.Now().Format("2006-01-02")
	msg := fmt.Sprintf("📊 NEVERLAND DAILY REPORT — %s\n\n", now)
	msg += fmt.Sprintf("TVL: $%s\n", formatNumber(snap.Metrics["tvl"]))
	msg += fmt.Sprintf("veDUST TVL: $%s\n", formatNumber(snap.Metrics["vedust_tvl"]))
	if p, ok := snap.Metrics["price"]; ok && p > 0 {
		msg += fmt.Sprintf("DUST Price: $%.4f\n", p)
	}
	msg += fmt.Sprintf("\nFees (24h): $%s\n", formatNumber(snap.Metrics["fees_24h"]))
	msg += fmt.Sprintf("Fees (7d): $%s\n", formatNumber(snap.Metrics["fees_7d"]))
	msg += fmt.Sprintf("Fees (30d): $%s\n", formatNumber(snap.Metrics["fees_30d"]))

	// Historical TVL from DefiLlama
	hist, err := n.fetchTVLHistory()
	if err == nil && len(hist) > 0 {
		currentTVL := snap.Metrics["tvl"]
		msg += "\nTVL History:\n"
		for _, p := range []struct {
			label string
			days  int
		}{{"1d", 1}, {"7d", 7}, {"30d", 30}} {
			if p.days < len(hist) {
				pastTVL := hist[len(hist)-1-p.days].TVL
				if pastTVL > 0 {
					pctChange := (currentTVL - pastTVL) / pastTVL * 100
					sign := "+"
					if pctChange < 0 {
						sign = ""
					}
					msg += fmt.Sprintf("  %s: $%s (%s%.1f%%)\n", p.label, formatNumber(pastTVL), sign, pctChange)
				}
			}
		}
	}

	msg += "\n🔗 https://app.neverland.money"
	return msg, nil
}

// --- API helpers ---

type neverlandProtocol struct {
	CurrentChainTvls struct {
		Monad   float64 `json:"Monad"`
		Staking float64 `json:"staking"`
	} `json:"currentChainTvls"`
}

func (n *Neverland) fetchProtocol() (*neverlandProtocol, error) {
	body, err := n.httpGet(neverlandDataAPI)
	if err != nil {
		return nil, err
	}
	var result neverlandProtocol
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("unmarshal protocol: %w", err)
	}
	return &result, nil
}

type neverlandFees struct {
	Total24h float64 `json:"total24h"`
	Total7d  float64 `json:"total7d"`
	Total30d float64 `json:"total30d"`
}

func (n *Neverland) fetchFees() (*neverlandFees, error) {
	body, err := n.httpGet(neverlandFeesAPI)
	if err != nil {
		return nil, err
	}
	var result neverlandFees
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("unmarshal fees: %w", err)
	}
	return &result, nil
}

type tvlPoint struct {
	Date int64   `json:"date"`
	TVL  float64 `json:"totalLiquidityUSD"`
}

func (n *Neverland) fetchTVLHistory() ([]tvlPoint, error) {
	body, err := n.httpGet(neverlandDataAPI)
	if err != nil {
		return nil, err
	}
	var raw struct {
		TVL []tvlPoint `json:"tvl"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal tvl history: %w", err)
	}
	return raw.TVL, nil
}

func (n *Neverland) fetchDustPrice() (float64, error) {
	body, err := n.httpGet(dustPriceAPI)
	if err != nil {
		return 0, err
	}
	var result struct {
		Pairs []struct {
			ChainID   string `json:"chainId"`
			DexID     string `json:"dexId"`
			BaseToken struct {
				Symbol string `json:"symbol"`
			} `json:"baseToken"`
			PriceUsd  string `json:"priceUsd"`
			Liquidity struct {
				Usd float64 `json:"usd"`
			} `json:"liquidity"`
		} `json:"pairs"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("unmarshal dexscreener: %w", err)
	}
	// Find DUST on Monad chain — pick the pair with the highest liquidity
	var bestPrice float64
	var bestLiquidity float64
	for _, p := range result.Pairs {
		if p.ChainID == "monad" && p.BaseToken.Symbol == "DUST" {
			var price float64
			if _, err := fmt.Sscanf(p.PriceUsd, "%f", &price); err != nil || price <= 0 {
				continue
			}
			if p.Liquidity.Usd > bestLiquidity {
				bestLiquidity = p.Liquidity.Usd
				bestPrice = price
			}
		}
	}
	if bestPrice > 0 {
		return bestPrice, nil
	}
	return 0, fmt.Errorf("DUST price not found on Monad")
}

func (n *Neverland) httpGet(url string) ([]byte, error) {
	resp, err := n.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}
