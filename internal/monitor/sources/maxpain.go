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

// MaxPain scrapes CoinGlass liquidation max pain data via headless Chrome.
type MaxPain struct {
	logger  *slog.Logger
	mu      sync.RWMutex
	entries map[string]monitor.MaxPainEntry // keyed by "SYMBOL:interval" e.g. "BTC:24h"
}

func NewMaxPain(logger *slog.Logger) *MaxPain {
	return &MaxPain{
		logger:  logger,
		entries: make(map[string]monitor.MaxPainEntry),
	}
}

func (m *MaxPain) Name() string  { return "maxpain" }
func (m *MaxPain) Chain() string { return "General" }
func (m *MaxPain) URL() string   { return maxpainURL }

// GetEntry returns the latest scraped max pain data for a coin and interval.
func (m *MaxPain) GetEntry(symbol, interval string) (monitor.MaxPainEntry, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	key := strings.ToUpper(symbol) + ":" + interval
	e, ok := m.entries[key]
	return e, ok
}

// FetchSnapshot scrapes CoinGlass for all intervals and returns top-coin metrics.
func (m *MaxPain) FetchSnapshot() (*monitor.Snapshot, error) {
	allEntries := make(map[string]monitor.MaxPainEntry)

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
func (m *MaxPain) scrapeInterval(interval string) ([]monitor.MaxPainEntry, error) {
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

func (m *MaxPain) scrapeIntervals(intervals []string) (map[string][]monitor.MaxPainEntry, error) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-crash-reporter", true),
		chromedp.Flag("crash-dumps-dir", "/tmp"),
		chromedp.UserDataDir("/tmp/chromedp-profile"),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer allocCancel()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	ctx, cancel = context.WithTimeout(ctx, time.Duration(30+20*len(intervals))*time.Second)
	defer cancel()

	// Navigate once; default view is 24h
	if err := chromedp.Run(ctx,
		chromedp.Navigate(maxpainURL),
		chromedp.WaitVisible(`table tbody tr`, chromedp.ByQuery),
		chromedp.Sleep(2*time.Second),
	); err != nil {
		return nil, fmt.Errorf("chromedp navigate: %w", err)
	}

	result := make(map[string][]monitor.MaxPainEntry, len(intervals))

	for _, iv := range intervals {
		// Click the tab button matching this interval value, then wait for table refresh
		if iv != "24h" {
			tabSelector := fmt.Sprintf(`button[value="%s"], [role="tab"][value="%s"]`, iv, iv)
			if err := chromedp.Run(ctx,
				chromedp.Click(tabSelector, chromedp.ByQuery),
				chromedp.Sleep(3*time.Second),
			); err != nil {
				m.logger.Warn("click maxpain tab failed", "interval", iv, "error", err)
				continue
			}
		}

		var resultJSON string
		if err := chromedp.Run(ctx,
			chromedp.Evaluate(extractJS, &resultJSON),
		); err != nil {
			m.logger.Warn("scrape maxpain interval failed", "interval", iv, "error", err)
			continue
		}

		var entries []monitor.MaxPainEntry
		if err := json.Unmarshal([]byte(resultJSON), &entries); err != nil {
			m.logger.Warn("parse maxpain interval failed", "interval", iv, "error", err)
			continue
		}

		if len(entries) == 0 {
			var debugHTML string
			_ = chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('table') ? document.querySelector('table').outerHTML.substring(0, 2000) : 'NO TABLE FOUND'`, &debugHTML))
			m.logger.Warn("maxpain table empty", "interval", iv, "table_html", debugHTML)
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
