package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/web3-frozen/onchain-monitor/internal/monitor"
)

const maxpainURL = "https://www.coinglass.com/liquidation-maxpain"

// MaxPainEntry holds liquidation max pain data for a single coin.
type MaxPainEntry struct {
	Symbol                   string  `json:"symbol"`
	Price                    float64 `json:"price"`
	MaxLongLiquidationPrice  float64 `json:"maxLongLiquidationPrice"`
	MaxShortLiquidationPrice float64 `json:"maxShortLiquidationPrice"`
}

// MaxPain scrapes CoinGlass liquidation max pain data via headless Chrome.
type MaxPain struct {
	logger  *slog.Logger
	mu      sync.RWMutex
	entries map[string]MaxPainEntry // keyed by uppercase symbol
}

func NewMaxPain(logger *slog.Logger) *MaxPain {
	return &MaxPain{
		logger:  logger,
		entries: make(map[string]MaxPainEntry),
	}
}

func (m *MaxPain) Name() string  { return "maxpain" }
func (m *MaxPain) Chain() string { return "General" }
func (m *MaxPain) URL() string   { return maxpainURL }

// GetEntry returns the latest scraped max pain data for a coin.
func (m *MaxPain) GetEntry(symbol string) (MaxPainEntry, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.entries[strings.ToUpper(symbol)]
	return e, ok
}

// FetchSnapshot scrapes CoinGlass and returns top-coin metrics as a snapshot.
func (m *MaxPain) FetchSnapshot() (*monitor.Snapshot, error) {
	entries, err := m.scrape()
	if err != nil {
		return nil, fmt.Errorf("scrape maxpain: %w", err)
	}

	m.mu.Lock()
	m.entries = make(map[string]MaxPainEntry, len(entries))
	for _, e := range entries {
		m.entries[strings.ToUpper(e.Symbol)] = e
	}
	m.mu.Unlock()

	metrics := make(map[string]float64)
	dataSources := make(map[string]string)
	for _, e := range entries {
		sym := strings.ToUpper(e.Symbol)
		metrics[sym+"_price"] = e.Price
		metrics[sym+"_long_maxpain"] = e.MaxLongLiquidationPrice
		metrics[sym+"_short_maxpain"] = e.MaxShortLiquidationPrice
		dataSources[sym+"_price"] = "CoinGlass"
		dataSources[sym+"_long_maxpain"] = "CoinGlass"
		dataSources[sym+"_short_maxpain"] = "CoinGlass"
	}

	return &monitor.Snapshot{
		Source:      "maxpain",
		Chain:       "General",
		Metrics:     metrics,
		DataSources: dataSources,
		FetchedAt:   time.Now(),
	}, nil
}

func (m *MaxPain) FetchDailyReport() (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.entries) == 0 {
		return "", fmt.Errorf("no maxpain data available")
	}

	var b strings.Builder
	b.WriteString("📊 Liquidation Max Pain Report\n\n")
	for _, sym := range []string{"BTC", "ETH", "SOL"} {
		e, ok := m.entries[sym]
		if !ok {
			continue
		}
		longDist := (e.MaxLongLiquidationPrice - e.Price) / e.Price * 100
		shortDist := (e.Price - e.MaxShortLiquidationPrice) / e.Price * 100
		b.WriteString(fmt.Sprintf("%s  $%s\n", sym, fmtNum(e.Price)))
		b.WriteString(fmt.Sprintf("  Long Max Pain:  $%s (%.1f%%)\n", fmtNum(e.MaxLongLiquidationPrice), longDist))
		b.WriteString(fmt.Sprintf("  Short Max Pain: $%s (%.1f%%)\n\n", fmtNum(e.MaxShortLiquidationPrice), shortDist))
	}
	b.WriteString("🔗 " + maxpainURL)
	return b.String(), nil
}

func fmtNum(v float64) string {
	if v >= 1_000_000 {
		return fmt.Sprintf("%.2fM", v/1_000_000)
	}
	if v >= 1_000 {
		return fmt.Sprintf("%.2f", math.Round(v*100)/100)
	}
	return fmt.Sprintf("%.4f", v)
}

// scrape uses headless Chrome to extract max pain data from CoinGlass.
func (m *MaxPain) scrape() ([]MaxPainEntry, error) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-dev-shm-usage", true),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer allocCancel()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	var resultJSON string
	err := chromedp.Run(ctx,
		chromedp.Navigate(maxpainURL),
		// Wait for the table body to appear (contains maxpain rows)
		chromedp.WaitVisible(`table tbody tr`, chromedp.ByQuery),
		chromedp.Sleep(2*time.Second),
		// Extract data from the rendered table via JS
		chromedp.Evaluate(extractJS, &resultJSON),
	)
	if err != nil {
		return nil, fmt.Errorf("chromedp: %w", err)
	}

	var entries []MaxPainEntry
	if err := json.Unmarshal([]byte(resultJSON), &entries); err != nil {
		return nil, fmt.Errorf("parse scraped data: %w", err)
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("no maxpain entries found")
	}

	m.logger.Info("scraped maxpain data", "coins", len(entries))
	return entries, nil
}

// extractJS is evaluated in the browser to pull max pain data from the rendered table.
const extractJS = `
(() => {
	const rows = document.querySelectorAll('table tbody tr');
	const data = [];
	rows.forEach(row => {
		const cells = row.querySelectorAll('td');
		if (cells.length < 5) return;
		// Column layout: Symbol | Price | Long Max Pain Price | Long Distance | Short Max Pain Price | Short Distance
		const symbol = (cells[0].textContent || '').trim().split(/\s/)[0];
		const parseNum = s => {
			s = (s || '').replace(/[$,\s]/g, '');
			const n = parseFloat(s);
			return isNaN(n) ? 0 : n;
		};
		const price = parseNum(cells[1].textContent);
		const longMaxPain = parseNum(cells[2].textContent);
		const shortMaxPain = parseNum(cells[4].textContent);
		if (symbol && price > 0) {
			data.push({
				symbol: symbol,
				price: price,
				maxLongLiquidationPrice: longMaxPain,
				maxShortLiquidationPrice: shortMaxPain,
			});
		}
	});
	return JSON.stringify(data);
})()
`
