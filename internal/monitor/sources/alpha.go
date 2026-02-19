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

const alphaAPI = "https://alpha123.uk/api/data?fresh=1"

type alphaAirdropResp struct {
	Token            string `json:"token"`
	Date             string `json:"date"`
	Time             string `json:"time"`
	Points           int    `json:"points"`
	Type             string `json:"type"`
	Name             string `json:"name"`
	CreatedTimestamp int64  `json:"created_timestamp"`
}

type alphaResp struct {
	Airdrops []alphaAirdropResp `json:"airdrops"`
}

// Alpha fetches events from alpha123.uk (airdrops, check-ins, etc.).
type Alpha struct {
	client   *http.Client
	baseURL  string
	mu       sync.RWMutex
	airdrops []monitor.AlphaAirdrop
}

func NewAlpha() *Alpha {
	return &Alpha{
		client:  &http.Client{Timeout: 15 * time.Second},
		baseURL: alphaAPI,
	}
}

func (a *Alpha) Name() string  { return "alpha" }
func (a *Alpha) Chain() string { return "General" }
func (a *Alpha) URL() string   { return "https://alpha123.uk/" }

func (a *Alpha) FetchSnapshot() (*monitor.Snapshot, error) {
	resp, err := a.client.Get(a.baseURL)
	if err != nil {
		return nil, fmt.Errorf("alpha API: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("alpha API status: %d", resp.StatusCode)
	}

	var body alphaResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode alpha: %w", err)
	}

	var ads []monitor.AlphaAirdrop
	topPoints := 0
	for _, it := range body.Airdrops {
		ad := monitor.AlphaAirdrop{
			Token:  it.Token,
			Date:   it.Date,
			Time:   it.Time,
			Points: it.Points,
			Name:   it.Name,
		}
		ads = append(ads, ad)
		if it.Points > topPoints {
			topPoints = it.Points
		}
	}

	a.mu.Lock()
	a.airdrops = ads
	a.mu.Unlock()

	return &monitor.Snapshot{
		Source: a.Name(),
		Chain:  a.Chain(),
		Metrics: map[string]float64{
			"airdrops":   float64(len(ads)),
			"top_points": float64(topPoints),
		},
		DataSources: map[string]string{
			"airdrops":   "alpha123.uk",
			"top_points": "alpha123.uk",
		},
		FetchedAt: time.Now(),
	}, nil
}

func (a *Alpha) GetAirdrops() []monitor.AlphaAirdrop {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]monitor.AlphaAirdrop, len(a.airdrops))
	copy(out, a.airdrops)
	return out
}

func (a *Alpha) FetchDailyReport() (string, error) {
	a.mu.RLock()
	ads := a.airdrops
	a.mu.RUnlock()
	if len(ads) == 0 {
		return "", fmt.Errorf("no alpha airdrops available")
	}
	var b strings.Builder
	b.WriteString("📣 Alpha Airdrops\n\n")
	for i, ad := range ads {
		b.WriteString(fmt.Sprintf("%d. %s — %s %s (%d points)\n", i+1, ad.Token, ad.Date, ad.Time, ad.Points))
	}
	return b.String(), nil
}
