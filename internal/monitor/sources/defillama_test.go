package sources

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDefiLlamaPool_WithdrawalDays(t *testing.T) {
	tests := []struct {
		name     string
		poolMeta *string
		want     int
	}{
		{"nil meta", nil, 0},
		{"empty meta", strPtr(""), 0},
		{"7 days unstaking", strPtr("7 days unstaking"), 7},
		{"3 day lockup", strPtr("3 day lockup"), 3},
		{"14d lock", strPtr("14d lock"), 14},
		{"1 week unstaking", strPtr("1 week unstaking"), 7},
		{"2 weeks lockup", strPtr("2 weeks lockup"), 14},
		{"cooldown mentioned", strPtr("has cooldown"), 7},
		{"syrup USDC", strPtr("Syrup USDC"), 0},
		{"no lock info", strPtr("regular pool"), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := DefiLlamaPool{PoolMeta: tt.poolMeta}
			if got := p.WithdrawalDays(); got != tt.want {
				t.Errorf("WithdrawalDays() = %v, want %v", got, tt.want)
			}
		})
	}
}

func strPtr(s string) *string {
	return &s
}

func TestDefiLlamaPool_IsUSDC(t *testing.T) {
	tests := []struct {
		symbol string
		want   bool
	}{
		{"USDC", true},
		{"USDC.e", true},
		{"aUSDC", true},
		{"USDT", false},
		{"DAI", false},
	}
	for _, tt := range tests {
		t.Run(tt.symbol, func(t *testing.T) {
			p := DefiLlamaPool{Symbol: tt.symbol}
			if got := p.IsUSDC(); got != tt.want {
				t.Errorf("IsUSDC() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDefiLlamaPool_IsUSDT(t *testing.T) {
	tests := []struct {
		symbol string
		want   bool
	}{
		{"USDT", true},
		{"USDT0", true},
		{"aUSDT", true},
		{"USDC", false},
		{"DAI", false},
	}
	for _, tt := range tests {
		t.Run(tt.symbol, func(t *testing.T) {
			p := DefiLlamaPool{Symbol: tt.symbol}
			if got := p.IsUSDT(); got != tt.want {
				t.Errorf("IsUSDT() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDefiLlama_FilterStablePools(t *testing.T) {
	pools := []DefiLlamaPool{
		{Pool: "1", Symbol: "USDC", APY: 5.0, TVLUsd: 2000000, Stablecoin: true, PoolMeta: nil},
		{Pool: "2", Symbol: "USDT", APY: 3.0, TVLUsd: 1000000, Stablecoin: true, PoolMeta: nil},
		{Pool: "3", Symbol: "USDC", APY: 10.0, TVLUsd: 500000, Stablecoin: true, PoolMeta: strPtr("7 days unstaking")},
		{Pool: "4", Symbol: "DAI", APY: 6.0, TVLUsd: 3000000, Stablecoin: true, PoolMeta: nil},
		{Pool: "5", Symbol: "ETH", APY: 8.0, TVLUsd: 5000000, Stablecoin: false, PoolMeta: nil},
	}

	d := NewDefiLlama(nil)

	// Test USDC only filter
	result := d.FilterStablePools(pools, 3, 500000, "USDC", 7)
	if len(result) != 2 {
		t.Errorf("USDC filter: got %d pools, want 2", len(result))
	}

	// Test USDT only filter
	result = d.FilterStablePools(pools, 3, 500000, "USDT", 7)
	if len(result) != 1 {
		t.Errorf("USDT filter: got %d pools, want 1", len(result))
	}

	// Test USDC_USDT filter
	result = d.FilterStablePools(pools, 3, 500000, "USDC_USDT", 7)
	if len(result) != 3 {
		t.Errorf("USDC_USDT filter: got %d pools, want 3", len(result))
	}

	// Test ALL_STABLES filter
	result = d.FilterStablePools(pools, 3, 500000, "ALL_STABLES", 7)
	if len(result) != 4 {
		t.Errorf("ALL_STABLES filter: got %d pools, want 4", len(result))
	}

	// Test immediate withdrawal only (0 days)
	result = d.FilterStablePools(pools, 3, 500000, "USDC", 0)
	if len(result) != 1 {
		t.Errorf("Immediate withdrawal: got %d pools, want 1", len(result))
	}

	// Test sorted by APY descending
	result = d.FilterStablePools(pools, 3, 500000, "USDC_USDT", 7)
	if len(result) > 1 && result[0].APY < result[1].APY {
		t.Errorf("Results not sorted by APY descending")
	}
}

func TestDefiLlama_FetchSnapshot(t *testing.T) {
	data := defillamaResponse{
		Status: "success",
		Data: []DefiLlamaPool{
			{Pool: "1", Symbol: "USDC", Chain: "Ethereum", Project: "aave-v3", APY: 4.5, TVLUsd: 1000000, Stablecoin: true},
			{Pool: "2", Symbol: "USDT", Chain: "Ethereum", Project: "compound", APY: 3.2, TVLUsd: 500000, Stablecoin: true},
			{Pool: "3", Symbol: "ETH", Chain: "Ethereum", Project: "lido", APY: 2.0, TVLUsd: 10000000, Stablecoin: false},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(data)
	}))
	defer srv.Close()

	d := NewDefiLlama(nil)
	d.baseURL = srv.URL

	snap, err := d.FetchSnapshot()
	if err != nil {
		t.Fatalf("FetchSnapshot() error: %v", err)
	}

	if snap.Source != "defillama" {
		t.Errorf("Source = %q, want defillama", snap.Source)
	}
	if snap.Chain != "General" {
		t.Errorf("Chain = %q, want General", snap.Chain)
	}
	if snap.Metrics["usdc_max_apy"] != 4.5 {
		t.Errorf("usdc_max_apy = %v, want 4.5", snap.Metrics["usdc_max_apy"])
	}
	if snap.Metrics["usdt_max_apy"] != 3.2 {
		t.Errorf("usdt_max_apy = %v, want 3.2", snap.Metrics["usdt_max_apy"])
	}
	if snap.Metrics["usdc_pools"] != 1 {
		t.Errorf("usdc_pools = %v, want 1", snap.Metrics["usdc_pools"])
	}
}

func TestDefiLlamaPool_ProjectDisplayName(t *testing.T) {
	tests := []struct {
		project string
		want    string
	}{
		{"aave-v3", "Aave V3"},
		{"compound", "Compound"},
		{"sky-lending", "Sky Lending"},
	}
	for _, tt := range tests {
		t.Run(tt.project, func(t *testing.T) {
			p := DefiLlamaPool{Project: tt.project}
			if got := p.ProjectDisplayName(); got != tt.want {
				t.Errorf("ProjectDisplayName() = %q, want %q", got, tt.want)
			}
		})
	}
}
