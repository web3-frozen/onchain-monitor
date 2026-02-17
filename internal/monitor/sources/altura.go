package sources

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/web3-frozen/onchain-monitor/internal/monitor"
)

const (
	alturaVaultAPI  = "https://api.subgraph.ormilabs.com/api/public/3c4075ed-8f9c-4f62-9c5b-68a0df8bd207/subgraphs/altura-vaultservice/0.0.3/gn"
	alturaOracleAPI = "https://api.subgraph.ormilabs.com/api/public/3c4075ed-8f9c-4f62-9c5b-68a0df8bd207/subgraphs/altura-oracle/0.0.1/gn"
	alturaLaunchDate = "2025-12-23T20:52:36Z"
	usdtDecimals     = 6
)

type Altura struct {
	client *http.Client
}

func NewAltura() *Altura {
	return &Altura{
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (a *Altura) Name() string { return "altura" }
func (a *Altura) URL() string  { return "https://app.altura.trade/stats" }

func (a *Altura) FetchSnapshot() (*monitor.Snapshot, error) {
	g, err := a.fetchGlobals()
	if err != nil {
		return nil, fmt.Errorf("fetch globals: %w", err)
	}

	pps, err := a.fetchLatestPPS()
	if err != nil {
		return nil, fmt.Errorf("fetch pps: %w", err)
	}

	tvl := parseAssets(g.TVLAssets)
	price := parsePPSUsd(g.LastOraclePpsUsd)
	apr := calcAPR(pps)

	return &monitor.Snapshot{
		Source: "altura",
		Metrics: map[string]float64{
			"tvl":   tvl,
			"price": price,
			"apr":   apr,
		},
		FetchedAt: time.Now(),
	}, nil
}

func (a *Altura) FetchDailyReport() (string, error) {
	snap, err := a.FetchSnapshot()
	if err != nil {
		return "", err
	}

	dayStats, err := a.fetchDayStats(30)
	if err != nil {
		return "", fmt.Errorf("fetch day stats: %w", err)
	}

	now := time.Now().Format("2006-01-02")
	msg := fmt.Sprintf("📊 ALTURA DAILY REPORT — %s\n\n", now)
	msg += fmt.Sprintf("TVL: $%s\n", formatNumber(snap.TVL()))
	msg += fmt.Sprintf("AVLT Price: $%.4f\n", snap.Price())
	msg += fmt.Sprintf("APR: %.2f%%\n", snap.APR())

	if len(dayStats) > 0 {
		msg += "\nTVL History:\n"
		currentTVL := snap.TVL()
		for _, period := range []struct {
			label string
			days  int
		}{{"1d", 1}, {"7d", 7}, {"30d", 30}} {
			if period.days < len(dayStats) {
				pastTVL := parseAssets(dayStats[period.days].TVLAssets)
				if pastTVL > 0 {
					pctChange := (currentTVL - pastTVL) / pastTVL * 100
					sign := "+"
					if pctChange < 0 {
						sign = ""
					}
					msg += fmt.Sprintf("  %s: $%s (%s%.1f%%)\n", period.label, formatNumber(pastTVL), sign, pctChange)
				}
			}
		}
	}

	msg += "\n🔗 https://app.altura.trade/stats"
	return msg, nil
}

// --- Internal GraphQL helpers ---

type globalsResponse struct {
	Data struct {
		Globals []struct {
			TVLAssets         string `json:"tvlAssets"`
			LastOraclePpsUsd  string `json:"lastOraclePpsUsd"`
			LastOracleUpdatedAt string `json:"lastOracleUpdatedAt"`
		} `json:"globals"`
	} `json:"data"`
}

func (a *Altura) fetchGlobals() (*struct {
	TVLAssets        string
	LastOraclePpsUsd string
}, error) {
	body := `{"query":"{ globals(first: 1) { tvlAssets lastOraclePpsUsd lastOracleUpdatedAt } }"}`
	resp, err := a.graphql(alturaVaultAPI, body)
	if err != nil {
		return nil, err
	}

	var result globalsResponse
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("unmarshal globals: %w", err)
	}
	if len(result.Data.Globals) == 0 {
		return nil, fmt.Errorf("no global data")
	}

	g := result.Data.Globals[0]
	return &struct {
		TVLAssets        string
		LastOraclePpsUsd string
	}{
		TVLAssets:        g.TVLAssets,
		LastOraclePpsUsd: g.LastOraclePpsUsd,
	}, nil
}

type oracleNavResponse struct {
	Data struct {
		OracleNavs []struct {
			PpsUsd    string `json:"ppsUsd"`
			Timestamp string `json:"timestamp"`
		} `json:"oracleNavs"`
	} `json:"data"`
}

func (a *Altura) fetchLatestPPS() (float64, error) {
	body := `{"query":"{ oracleNavs(first: 1, orderBy: timestamp, orderDirection: desc) { ppsUsd timestamp } }"}`
	resp, err := a.graphql(alturaOracleAPI, body)
	if err != nil {
		return 0, err
	}

	var result oracleNavResponse
	if err := json.Unmarshal(resp, &result); err != nil {
		return 0, fmt.Errorf("unmarshal oracle nav: %w", err)
	}
	if len(result.Data.OracleNavs) == 0 {
		return 0, fmt.Errorf("no oracle nav data")
	}

	return strconv.ParseFloat(result.Data.OracleNavs[0].PpsUsd, 64)
}

type dayStatsResponse struct {
	Data struct {
		DayStats []struct {
			ID        string `json:"id"`
			Date      int64  `json:"date"`
			TVLAssets string `json:"tvlAssets"`
		} `json:"dayStats"`
	} `json:"data"`
}

func (a *Altura) fetchDayStats(count int) ([]struct {
	TVLAssets string
}, error) {
	query := fmt.Sprintf(`{"query":"{ dayStats(first: %d, orderBy: date, orderDirection: desc) { id date tvlAssets } }"}`, count)
	resp, err := a.graphql(alturaVaultAPI, query)
	if err != nil {
		return nil, err
	}

	var result dayStatsResponse
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("unmarshal day stats: %w", err)
	}

	out := make([]struct{ TVLAssets string }, len(result.Data.DayStats))
	for i, ds := range result.Data.DayStats {
		out[i].TVLAssets = ds.TVLAssets
	}
	return out, nil
}

func (a *Altura) graphql(url, body string) ([]byte, error) {
	req, err := http.NewRequest("POST", url, bytes.NewBufferString(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("graphql request failed: %d", resp.StatusCode)
	}

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// --- Helpers ---

func parseAssets(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v / math.Pow(10, usdtDecimals)
}

func parsePPSUsd(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func calcAPR(pps float64) float64 {
	launch, _ := time.Parse(time.RFC3339, alturaLaunchDate)
	elapsed := time.Since(launch).Seconds() / 31536000 // seconds per year
	if elapsed <= 0 || pps <= 1 {
		return 0
	}
	return (math.Pow(pps, 1/elapsed) - 1) * 100
}

func formatNumber(v float64) string {
	if v >= 1_000_000 {
		return fmt.Sprintf("%.2fM", v/1_000_000)
	}
	if v >= 1_000 {
		return fmt.Sprintf("%.2fK", v/1_000)
	}
	return fmt.Sprintf("%.2f", v)
}
