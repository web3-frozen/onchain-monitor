package sources

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/web3-frozen/onchain-monitor/internal/monitor"
)

const fngAPI = "https://api.alternative.me/fng/"

type FearGreed struct {
	client  *http.Client
	baseURL string
}

func NewFearGreed() *FearGreed {
	return &FearGreed{
		client:  &http.Client{Timeout: 15 * time.Second},
		baseURL: fngAPI,
	}
}

func (f *FearGreed) Name() string  { return "general" }
func (f *FearGreed) Chain() string { return "General" }
func (f *FearGreed) URL() string   { return "https://alternative.me/crypto/fear-and-greed-index/" }

type fngResponse struct {
	Data []struct {
		Value               string `json:"value"`
		ValueClassification string `json:"value_classification"`
	} `json:"data"`
}

func (f *FearGreed) FetchSnapshot() (*monitor.Snapshot, error) {
	resp, err := f.client.Get(f.baseURL)
	if err != nil {
		return nil, fmt.Errorf("fear & greed API: %w", err)
	}
	defer resp.Body.Close()

	var fng fngResponse
	if err := json.NewDecoder(resp.Body).Decode(&fng); err != nil {
		return nil, fmt.Errorf("decode fear & greed: %w", err)
	}
	if len(fng.Data) == 0 {
		return nil, fmt.Errorf("no fear & greed data")
	}

	val, err := strconv.ParseFloat(fng.Data[0].Value, 64)
	if err != nil {
		return nil, fmt.Errorf("parse fear & greed value: %w", err)
	}

	return &monitor.Snapshot{
		Source: f.Name(),
		Chain:  f.Chain(),
		Metrics: map[string]float64{
			"fear_greed_index": val,
		},
		DataSources: map[string]string{
			"fear_greed_index": "Alternative.me",
		},
		FetchedAt: time.Now(),
	}, nil
}

func (f *FearGreed) FetchDailyReport() (string, error) {
	snap, err := f.FetchSnapshot()
	if err != nil {
		return "", err
	}

	val := snap.Metrics["fear_greed_index"]
	classification := classifyFng(val)
	now := time.Now().Format("2006-01-02")

	msg := fmt.Sprintf("📊 CRYPTO FEAR & GREED INDEX — %s\n\n", now)
	msg += fmt.Sprintf("Index: %.0f / 100\n", val)
	msg += fmt.Sprintf("Sentiment: %s\n\n", classification)
	msg += "🔗 https://alternative.me/crypto/fear-and-greed-index/"
	return msg, nil
}

func classifyFng(v float64) string {
	switch {
	case v <= 25:
		return "😱 Extreme Fear"
	case v <= 45:
		return "😰 Fear"
	case v <= 55:
		return "😐 Neutral"
	case v <= 75:
		return "😀 Greed"
	default:
		return "🤑 Extreme Greed"
	}
}
