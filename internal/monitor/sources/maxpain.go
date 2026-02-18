package sources

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/web3-frozen/onchain-monitor/internal/monitor"
)

const maxpainURL = "https://www.coinglass.com/liquidation-maxpain"
const maxpainAPI = "https://fapi.coinglass.com/api/liqHeatMap/list"

// Bearer token extracted from CoinGlass frontend JS (public, embedded in client-side code).
const cgBearerToken = "REDACTED_TOKEN"

// MaxPain fetches CoinGlass liquidation max pain data via their internal API.
type MaxPain struct {
	logger  *slog.Logger
	client  *http.Client
	mu      sync.RWMutex
	entries map[string]monitor.MaxPainEntry // keyed by "SYMBOL:interval" e.g. "BTC:24h"
}

func NewMaxPain(logger *slog.Logger) *MaxPain {
	return &MaxPain{
		logger:  logger,
		client:  &http.Client{Timeout: 30 * time.Second},
		entries: make(map[string]monitor.MaxPainEntry),
	}
}

func (m *MaxPain) Name() string  { return "maxpain" }
func (m *MaxPain) Chain() string { return "General" }
func (m *MaxPain) URL() string   { return maxpainURL }

// GetEntry returns the latest max pain data for a coin and interval.
func (m *MaxPain) GetEntry(symbol, interval string) (monitor.MaxPainEntry, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	key := strings.ToUpper(symbol) + ":" + interval
	e, ok := m.entries[key]
	return e, ok
}

// FetchSnapshot fetches CoinGlass API for 24h interval and returns top-coin metrics.
func (m *MaxPain) FetchSnapshot() (*monitor.Snapshot, error) {
	allEntries := make(map[string]monitor.MaxPainEntry)

	for _, interval := range []string{"24h"} {
		entries, err := m.fetchInterval(interval)
		if err != nil {
			return nil, fmt.Errorf("fetch maxpain %s: %w", interval, err)
		}
		for _, e := range entries {
			key := strings.ToUpper(e.Symbol) + ":" + interval
			e.Interval = interval
			allEntries[key] = e
		}
	}

	m.mu.Lock()
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

// ScrapeInterval fetches a specific interval and updates the cache.
func (m *MaxPain) ScrapeInterval(interval string) error {
	entries, err := m.fetchInterval(interval)
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

// ScrapeIntervals fetches multiple intervals.
func (m *MaxPain) ScrapeIntervals(intervals []string) error {
	for _, iv := range intervals {
		if err := m.ScrapeInterval(iv); err != nil {
			return err
		}
	}
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

// cgAPIResponse is the raw JSON structure from the CoinGlass API.
type cgAPIResponse struct {
	Code    string `json:"code"`
	Msg     string `json:"msg"`
	Success bool   `json:"success"`
	Data    string `json:"data"` // AES-ECB encrypted, gzip-compressed JSON
}

// cgMaxPainItem is a single coin entry from the decrypted API response.
type cgMaxPainItem struct {
	Symbol                   string  `json:"symbol"`
	Price                    float64 `json:"price"`
	MaxLongLiquidationPrice  float64 `json:"maxLongLiquidationPrice"`
	MaxShortLiquidationPrice float64 `json:"maxShortLiquidationPrice"`
}

// fetchInterval calls the CoinGlass API, decrypts and parses the response.
func (m *MaxPain) fetchInterval(interval string) ([]monitor.MaxPainEntry, error) {
	req, err := http.NewRequest("GET", maxpainAPI+"?range="+interval, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", "https://www.coinglass.com")
	req.Header.Set("Referer", "https://www.coinglass.com/liquidation-maxpain")
	req.Header.Set("Authorization", "Bearer "+cgBearerToken)
	req.Header.Set("cache-ts-v2", fmt.Sprintf("%d", time.Now().UnixMilli()))

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	var apiResp cgAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	if apiResp.Code != "0" || !apiResp.Success {
		return nil, fmt.Errorf("API error: code=%s msg=%s", apiResp.Code, apiResp.Msg)
	}
	if apiResp.Data == "" {
		return nil, fmt.Errorf("API returned empty data (may need auth token update)")
	}

	// Check for encryption header
	enc := resp.Header.Get("encryption")
	v := resp.Header.Get("v")
	userHeader := resp.Header.Get("user")

	if enc != "true" || userHeader == "" {
		return nil, fmt.Errorf("response not encrypted (encryption=%s, user=%s)", enc, userHeader)
	}

	// Decrypt: derive initial key from API path
	apiPath := "/api/liqHeatMap/list"
	var initKey string
	if v == "0" {
		// Use cache-ts-v2 request header
		initKey = req.Header.Get("cache-ts-v2")
	} else if v == "2" {
		// Use response time header
		initKey = resp.Header.Get("time")
	} else {
		// v=1 or other: use base64 of API path
		initKey = apiPath
	}
	s := base64.StdEncoding.EncodeToString([]byte(initKey))
	if len(s) > 16 {
		s = s[:16]
	}

	// Decrypt 'user' header → get real AES key
	decUserHex, err := aesECBDecryptToHex(userHeader, s)
	if err != nil {
		return nil, fmt.Errorf("decrypt user header: %w", err)
	}
	realKeyBytes, err := hex.DecodeString(decUserHex)
	if err != nil {
		return nil, fmt.Errorf("hex decode user: %w", err)
	}
	// Decompress gzipped key
	realKey, err := gunzipBytes(realKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("gunzip user key: %w", err)
	}

	// Decrypt data.data with real key
	decDataHex, err := aesECBDecryptToHex(apiResp.Data, string(realKey))
	if err != nil {
		return nil, fmt.Errorf("decrypt data: %w", err)
	}
	dataBytes, err := hex.DecodeString(decDataHex)
	if err != nil {
		return nil, fmt.Errorf("hex decode data: %w", err)
	}
	// Decompress gzipped data
	jsonData, err := gunzipBytes(dataBytes)
	if err != nil {
		return nil, fmt.Errorf("gunzip data: %w", err)
	}

	var items []cgMaxPainItem
	if err := json.Unmarshal(jsonData, &items); err != nil {
		return nil, fmt.Errorf("unmarshal items: %w", err)
	}

	entries := make([]monitor.MaxPainEntry, 0, len(items))
	for _, item := range items {
		if item.Symbol == "" || item.Price <= 0 {
			continue
		}
		entries = append(entries, monitor.MaxPainEntry{
			Symbol:                   item.Symbol,
			Price:                    item.Price,
			MaxLongLiquidationPrice:  item.MaxLongLiquidationPrice,
			MaxShortLiquidationPrice: item.MaxShortLiquidationPrice,
		})
	}

	m.logger.Info("fetched maxpain data", "interval", interval, "coins", len(entries))
	return entries, nil
}

// aesECBDecryptToHex decrypts a base64-encoded ciphertext with AES-ECB and returns hex string.
func aesECBDecryptToHex(cipherB64, key string) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(cipherB64)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", fmt.Errorf("aes new cipher: %w", err)
	}
	bs := block.BlockSize()
	if len(ciphertext)%bs != 0 {
		return "", fmt.Errorf("ciphertext length %d not multiple of block size %d", len(ciphertext), bs)
	}
	plaintext := make([]byte, len(ciphertext))
	for i := 0; i < len(ciphertext); i += bs {
		block.Decrypt(plaintext[i:i+bs], ciphertext[i:i+bs])
	}
	// Remove PKCS7 padding
	if len(plaintext) > 0 {
		pad := int(plaintext[len(plaintext)-1])
		if pad > 0 && pad <= bs {
			plaintext = plaintext[:len(plaintext)-pad]
		}
	}
	return hex.EncodeToString(plaintext), nil
}

// gunzipBytes decompresses gzip data.
func gunzipBytes(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}
