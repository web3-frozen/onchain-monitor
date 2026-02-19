package sources

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFormatBinancePrice(t *testing.T) {
	tests := []struct {
		input float64
		want  string
	}{
		{0.0045, "0.0045"},
		{0.5, "0.5000"},
		{999.99, "999.9900"},
		{1000, "1,000.00"},
		{12345.67, "12,345.67"},
		{95432.10, "95,432.10"},
		{100000.00, "100,000.00"},
	}
	for _, tt := range tests {
		got := formatBinancePrice(tt.input)
		if got != tt.want {
			t.Errorf("formatBinancePrice(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFetchPrice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		symbol := r.URL.Query().Get("symbol")
		if symbol == "BTCUSDT" {
			json.NewEncoder(w).Encode(binanceTickerResp{Symbol: "BTCUSDT", Price: "95432.10"})
			return
		}
		if symbol == "INVALIDUSDT" {
			http.Error(w, "bad symbol", http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(binanceTickerResp{Symbol: symbol, Price: "3456.78"})
	}))
	defer srv.Close()

	b := &Binance{client: srv.Client(), baseURL: srv.URL}

	// Happy path
	price, err := b.FetchPrice("BTC")
	if err != nil {
		t.Fatalf("FetchPrice(BTC) error: %v", err)
	}
	if price != 95432.10 {
		t.Errorf("FetchPrice(BTC) = %v, want 95432.10", price)
	}

	// Another symbol
	price, err = b.FetchPrice("ETH")
	if err != nil {
		t.Fatalf("FetchPrice(ETH) error: %v", err)
	}
	if price != 3456.78 {
		t.Errorf("FetchPrice(ETH) = %v, want 3456.78", price)
	}

	// Bad status code
	_, err = b.FetchPrice("INVALID")
	if err == nil {
		t.Error("FetchPrice(INVALID) expected error, got nil")
	}
}

func TestFetchSnapshot(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(binanceTickerResp{Symbol: "BTCUSDT", Price: "99000.50"})
	}))
	defer srv.Close()

	b := &Binance{client: srv.Client(), baseURL: srv.URL}
	snap, err := b.FetchSnapshot()
	if err != nil {
		t.Fatalf("FetchSnapshot error: %v", err)
	}
	if snap.Source != "binance" {
		t.Errorf("Source = %q, want %q", snap.Source, "binance")
	}
	if snap.Chain != "General" {
		t.Errorf("Chain = %q, want %q", snap.Chain, "General")
	}
	if snap.Metrics["btc_price"] != 99000.50 {
		t.Errorf("btc_price = %v, want 99000.50", snap.Metrics["btc_price"])
	}
}
