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

// maxpainIntervals maps window_minutes to CoinGlass type param.
var maxpainIntervals = map[int]string{
	720:   "12h",
	1440:  "24h",
	2880:  "48h",
	4320:  "3d",
	10080: "7d",
}

// MaxPainEntry holds liquidation max pain data for a single coin.
type MaxPainEntry struct {
	Symbol                   string  `json:"symbol"`
	Price                    float64 `json:"price"`
	MaxLongLiquidationPrice  float64 `json:"maxLongLiquidationPrice"`
	MaxShortLiquidationPrice float64 `json:"maxShortLiquidationPrice"`
	Interval                 string  `json:"interval"` // e.g. "24h"
}

// MaxPain scrapes CoinGlass liquidation max pain data via headless Chrome.
type MaxPain struct {
	logger  *slog.Logger
	mu      sync.RWMutex
	entries map[string]MaxPainEntry // keyed by "SYMBOL:interval" e.g. "BTC:24h"
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

// GetEntry returns the latest scraped max pain data for a coin and interval.
func (m *MaxPain) GetEntry(symbol, interval string) (MaxPainEntry, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	key := strings.ToUpper(symbol) + ":" + interval
	e, ok := m.entries[key]
	return e, ok
}

// IntervalFromMinutes converts window_minutes to a CoinGlass interval string.
func IntervalFromMinutes(minutes int) string {
	if iv, ok := maxpainIntervals[minutes]; ok {
		return iv
	}
	return "24h" // default
}

// FetchSnapshot scrapes CoinGlass for all intervals and returns top-coin metrics.
func (m *MaxPain) FetchSnapshot() (*monitor.Snapshot, error) {
	allEntries := make(map[string]MaxPainEntry)

	for _, interval := range []string{"24h"} {
		// Default snapshot only scrapes 24h; other intervals scraped on-demand by alerts
		entries, err := m.scrapeInterval(interval)
		if err != nil {
			return nil, fmt.Errorf("scrape maxpain %s: %w", interval, err)
		}
		for _, e := range entries {
			key := strings.ToUpper(e.Symbol) + ":" + interval
			e.Interval = interval
			allEntries[key] = e
		}
	}

	m.mu.Lock()
	// Merge new entries (preserve other intervals already scraped)
	for k, v := range allEntries {
		m.entries[k] = v
	}
	m.mu.Unlock()

	metrics := make(map[string]float64)
	dataSources := make(map[string]string)
	for _, e := range allEntries {
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

// ScrapeInterval scrapes a specific interval and updates the cache.
func (m *MaxPain) ScrapeInterval(interval string) error {
	entries, err := m.scrapeInterval(interval)
	if err != nil {
		return err
	}
	m.mu.Lock()
	for _, e := range entries {
		key := strings.ToUpper(e.Symbol) + ":" + interval
		e.Interval = interval
		m.entries[key] = e
	}
	m.mu.Unlock()
	return nil
}

func (m *MaxPain) FetchDailyReport() (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.entries) == 0 {
		return "", fmt.Errorf("no maxpain data available")
	}

	var b strings.Builder
	b.WriteString("📊 Liquidation Max Pain Report (24h)\n\n")
	for _, sym := range []string{"BTC", "ETH", "SOL"} {
		e, ok := m.entries[sym+":24h"]
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

// scrapeInterval uses headless Chrome to extract max pain data for a given interval.
func (m *MaxPain) scrapeInterval(interval string) ([]MaxPainEntry, error) {
	result, err := m.scrapeIntervals([]string{interval})
	if err != nil {
		return nil, err
	}
	return result[interval], nil
}

// ScrapeIntervals scrapes multiple intervals in a single Chrome session.
func (m *MaxPain) ScrapeIntervals(intervals []string) error {
	result, err := m.scrapeIntervals(intervals)
	if err != nil {
		return err
	}
	m.mu.Lock()
	for iv, entries := range result {
		for _, e := range entries {
			key := strings.ToUpper(e.Symbol) + ":" + iv
			e.Interval = iv
			m.entries[key] = e
		}
	}
	m.mu.Unlock()
	return nil
}

func (m *MaxPain) scrapeIntervals(intervals []string) (map[string][]MaxPainEntry, error) {
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

	ctx, cancel = context.WithTimeout(ctx, time.Duration(30+30*len(intervals))*time.Second)
	defer cancel()

	result := make(map[string][]MaxPainEntry, len(intervals))

	for _, iv := range intervals {
		var resultJSON string
		err := chromedp.Run(ctx,
			chromedp.Navigate(maxpainURL+"?type="+iv),
			chromedp.WaitVisible(`table tbody tr`, chromedp.ByQuery),
			chromedp.Sleep(2*time.Second),
			chromedp.Evaluate(extractJS, &resultJSON),
		)
		if err != nil {
			m.logger.Warn("scrape maxpain interval failed", "interval", iv, "error", err)
			continue
		}

		var entries []MaxPainEntry
		if err := json.Unmarshal([]byte(resultJSON), &entries); err != nil {
			m.logger.Warn("parse maxpain interval failed", "interval", iv, "error", err)
			continue
		}

		result[iv] = entries
		m.logger.Info("scraped maxpain data", "interval", iv, "coins", len(entries))
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("no maxpain data scraped")
	}
	return result, nil
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
