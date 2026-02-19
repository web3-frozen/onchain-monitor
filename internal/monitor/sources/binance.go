package sources

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/web3-frozen/onchain-monitor/internal/monitor"
)

const binanceTickerAPI = "https://api.binance.com/api/v3/ticker/price"

type binanceTickerResp struct {
	Symbol string `json:"symbol"`
	Price  string `json:"price"`
}

// Binance fetches cryptocurrency prices from the Binance public API.
type Binance struct {
	client *http.Client
}

func NewBinance() *Binance {
	return &Binance{
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (b *Binance) Name() string  { return "binance" }
func (b *Binance) Chain() string { return "General" }
func (b *Binance) URL() string   { return "https://www.binance.com/en/trade/" }

// FetchPrice fetches the current price for a symbol pair from Binance.
// symbol should be uppercase without quote asset (e.g., "BTC").
// It pairs with USDT by default.
func (b *Binance) FetchPrice(symbol string) (float64, error) {
	pair := strings.ToUpper(symbol) + "USDT"
	url := fmt.Sprintf("%s?symbol=%s", binanceTickerAPI, pair)

	resp, err := b.client.Get(url)
	if err != nil {
		return 0, fmt.Errorf("binance API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("binance API status: %d", resp.StatusCode)
	}

	var ticker binanceTickerResp
	if err := json.NewDecoder(resp.Body).Decode(&ticker); err != nil {
		return 0, fmt.Errorf("decode binance ticker: %w", err)
	}

	price, err := strconv.ParseFloat(ticker.Price, 64)
	if err != nil {
		return 0, fmt.Errorf("parse binance price: %w", err)
	}
	return price, nil
}

func (b *Binance) FetchSnapshot() (*monitor.Snapshot, error) {
	price, err := b.FetchPrice("BTC")
	if err != nil {
		return nil, err
	}

	return &monitor.Snapshot{
		Source: b.Name(),
		Chain:  b.Chain(),
		Metrics: map[string]float64{
			"btc_price": price,
		},
		DataSources: map[string]string{
			"btc_price": "Binance",
		},
		FetchedAt: time.Now(),
	}, nil
}

func (b *Binance) FetchDailyReport() (string, error) {
	price, err := b.FetchPrice("BTC")
	if err != nil {
		return "", err
	}

	now := time.Now().Format("2006-01-02")
	return fmt.Sprintf("📊 BINANCE BTC PRICE — %s\n\nBTC/USDT: $%s\n\n🔗 https://www.binance.com/en/trade/BTC_USDT",
		now, formatBinancePrice(price)), nil
}

func formatBinancePrice(v float64) string {
	if v >= 1_000 {
		intPart := fmt.Sprintf("%.2f", v)
		parts := strings.SplitN(intPart, ".", 2)
		n := len(parts[0])
		var result []byte
		for i, c := range parts[0] {
			if i > 0 && (n-i)%3 == 0 {
				result = append(result, ',')
			}
			result = append(result, byte(c))
		}
		if len(parts) == 2 {
			return string(result) + "." + parts[1]
		}
		return string(result)
	}
	return fmt.Sprintf("%.4f", v)
}
