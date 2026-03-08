package sources

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestDefiLlamaPool_DefiLlamaURL(t *testing.T) {
	p := DefiLlamaPool{}
	if got := p.DefiLlamaURL(); got != "https://defillama.com/yields" {
		t.Errorf("DefiLlamaURL() = %q, want https://defillama.com/yields", got)
	}
}

func TestDefiLlama_NameChainURL(t *testing.T) {
	d := NewDefiLlama(nil)
	if d.Name() != "defillama" {
		t.Errorf("Name() = %q, want defillama", d.Name())
	}
	if d.Chain() != "General" {
		t.Errorf("Chain() = %q, want General", d.Chain())
	}
	if d.URL() != "https://defillama.com/yields" {
		t.Errorf("URL() = %q, want https://defillama.com/yields", d.URL())
	}
}

func TestDefiLlama_FetchAllPools(t *testing.T) {
	data := defillamaResponse{
		Status: "success",
		Data: []DefiLlamaPool{
			{Pool: "1", Symbol: "USDC", APY: 4.5, TVLUsd: 1000000, Stablecoin: true},
			{Pool: "2", Symbol: "USDT", APY: 3.2, TVLUsd: 500000, Stablecoin: true},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(data)
	}))
	defer srv.Close()

	d := NewDefiLlama(nil)
	d.baseURL = srv.URL

	pools, err := d.FetchAllPools()
	if err != nil {
		t.Fatalf("FetchAllPools() error: %v", err)
	}
	if len(pools) != 2 {
		t.Errorf("got %d pools, want 2", len(pools))
	}
}

func TestDefiLlama_FetchAllPools_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	d := NewDefiLlama(nil)
	d.baseURL = srv.URL

	_, err := d.FetchAllPools()
	if err == nil {
		t.Fatal("expected error for 500 status")
	}
}

func TestDefiLlama_FetchAllPools_NonSuccessStatus(t *testing.T) {
	data := defillamaResponse{Status: "error"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(data)
	}))
	defer srv.Close()

	d := NewDefiLlama(nil)
	d.baseURL = srv.URL

	_, err := d.FetchAllPools()
	if err == nil {
		t.Fatal("expected error for non-success status")
	}
}

func TestDefiLlama_FilterStablePools_EmptyPools(t *testing.T) {
	d := NewDefiLlama(nil)
	result := d.FilterStablePools(nil, 3, 500000, "USDC", 7)
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d", len(result))
	}
}

func TestDefiLlama_FilterStablePools_MinAPY(t *testing.T) {
	d := NewDefiLlama(nil)
	pools := []DefiLlamaPool{
		{Pool: "1", Symbol: "USDC", APY: 2.0, TVLUsd: 1000000, Stablecoin: true},
		{Pool: "2", Symbol: "USDC", APY: 5.0, TVLUsd: 1000000, Stablecoin: true},
	}
	result := d.FilterStablePools(pools, 3, 500000, "USDC", 7)
	if len(result) != 1 || result[0].Pool != "2" {
		t.Errorf("expected only pool 2 (APY 5%%), got %d pools", len(result))
	}
}

func TestDefiLlama_FilterStablePools_MinTVL(t *testing.T) {
	d := NewDefiLlama(nil)
	pools := []DefiLlamaPool{
		{Pool: "1", Symbol: "USDC", APY: 5.0, TVLUsd: 100000, Stablecoin: true},
		{Pool: "2", Symbol: "USDC", APY: 5.0, TVLUsd: 2000000, Stablecoin: true},
	}
	result := d.FilterStablePools(pools, 3, 500000, "USDC", 7)
	if len(result) != 1 || result[0].Pool != "2" {
		t.Errorf("expected only pool 2 (TVL 2M), got %d pools", len(result))
	}
}

func TestDefiLlama_FilterStablePools_DefaultFilter(t *testing.T) {
	d := NewDefiLlama(nil)
	pools := []DefiLlamaPool{
		{Pool: "1", Symbol: "USDC", APY: 5.0, TVLUsd: 1000000, Stablecoin: true},
		{Pool: "2", Symbol: "ETH", APY: 5.0, TVLUsd: 1000000, Stablecoin: false},
	}
	// Unknown filter falls back to stablecoin check
	result := d.FilterStablePools(pools, 3, 500000, "UNKNOWN", 7)
	if len(result) != 1 || result[0].Pool != "1" {
		t.Errorf("default filter should require stablecoin=true, got %d pools", len(result))
	}
}

func TestDefiLlama_GetPools(t *testing.T) {
	d := NewDefiLlama(nil)
	d.pools = []DefiLlamaPool{
		{Pool: "1", Symbol: "USDC"},
		{Pool: "2", Symbol: "USDT"},
	}
	pools := d.GetPools()
	if len(pools) != 2 {
		t.Errorf("GetPools() returned %d, want 2", len(pools))
	}
	// Verify it returns a copy
	pools[0].Symbol = "MODIFIED"
	if d.pools[0].Symbol == "MODIFIED" {
		t.Error("GetPools() should return a copy, not a reference")
	}
}

func TestDefiLlama_GetFilteredPools(t *testing.T) {
	data := defillamaResponse{
		Status: "success",
		Data: []DefiLlamaPool{
			{Pool: "1", Symbol: "USDC", Chain: "Ethereum", Project: "aave-v3", APY: 5.0, TVLUsd: 2000000, Stablecoin: true},
			{Pool: "2", Symbol: "USDT", Chain: "Ethereum", Project: "compound", APY: 3.0, TVLUsd: 1000000, Stablecoin: true},
			{Pool: "3", Symbol: "ETH", Chain: "Ethereum", Project: "lido", APY: 8.0, TVLUsd: 5000000, Stablecoin: false},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(data)
	}))
	defer srv.Close()

	d := NewDefiLlama(nil)
	d.baseURL = srv.URL

	opps := d.GetFilteredPools(3, 500000, "USDC_USDT", 7)
	if len(opps) != 2 {
		t.Errorf("GetFilteredPools() returned %d, want 2", len(opps))
	}
	// Verify DefiLlamaOpp fields
	if opps[0].Project != "Aave V3" {
		t.Errorf("opps[0].Project = %q, want Aave V3", opps[0].Project)
	}
	if opps[0].URL != "https://defillama.com/yields" {
		t.Errorf("opps[0].URL = %q, want https://defillama.com/yields", opps[0].URL)
	}
}

func TestDefiLlama_FetchSnapshot_AllStableMetrics(t *testing.T) {
	data := defillamaResponse{
		Status: "success",
		Data: []DefiLlamaPool{
			{Pool: "1", Symbol: "USDC", Chain: "Ethereum", Project: "aave-v3", APY: 4.5, TVLUsd: 1000000, Stablecoin: true},
			{Pool: "2", Symbol: "USDT", Chain: "Ethereum", Project: "compound", APY: 3.2, TVLUsd: 500000, Stablecoin: true},
			{Pool: "3", Symbol: "DAI", Chain: "Ethereum", Project: "spark", APY: 6.0, TVLUsd: 2000000, Stablecoin: true},
			{Pool: "4", Symbol: "ETH", Chain: "Ethereum", Project: "lido", APY: 2.0, TVLUsd: 10000000, Stablecoin: false},
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

	// USDC/USDT metrics
	if snap.Metrics["usdc_max_apy"] != 4.5 {
		t.Errorf("usdc_max_apy = %v, want 4.5", snap.Metrics["usdc_max_apy"])
	}
	if snap.Metrics["usdt_max_apy"] != 3.2 {
		t.Errorf("usdt_max_apy = %v, want 3.2", snap.Metrics["usdt_max_apy"])
	}
	if snap.Metrics["usdc_pools"] != 1 {
		t.Errorf("usdc_pools = %v, want 1", snap.Metrics["usdc_pools"])
	}
	if snap.Metrics["usdt_pools"] != 1 {
		t.Errorf("usdt_pools = %v, want 1", snap.Metrics["usdt_pools"])
	}
	if snap.Metrics["total_pools"] != 2 {
		t.Errorf("total_pools = %v, want 2", snap.Metrics["total_pools"])
	}

	// All-stable metrics (USDC + USDT + DAI = 3 pools, max APY = DAI 6.0%)
	if snap.Metrics["all_stable_max_apy"] != 6.0 {
		t.Errorf("all_stable_max_apy = %v, want 6.0", snap.Metrics["all_stable_max_apy"])
	}
	if snap.Metrics["all_stable_pools"] != 3 {
		t.Errorf("all_stable_pools = %v, want 3", snap.Metrics["all_stable_pools"])
	}

	// Verify data sources
	if snap.DataSources["all_stable_max_apy"] != "DeFi Llama" {
		t.Errorf("all_stable_max_apy data source = %q, want DeFi Llama", snap.DataSources["all_stable_max_apy"])
	}
}

func TestDefiLlama_FetchDailyReport(t *testing.T) {
	d := NewDefiLlama(nil)
	d.pools = []DefiLlamaPool{
		{Pool: "1", Symbol: "USDC", Chain: "Ethereum", Project: "aave-v3", APY: 5.0, TVLUsd: 2000000, Stablecoin: true},
		{Pool: "2", Symbol: "USDT", Chain: "BSC", Project: "venus", APY: 3.0, TVLUsd: 1000000, Stablecoin: true, PoolMeta: strPtr("7 days unstaking")},
	}

	report, err := d.FetchDailyReport()
	if err != nil {
		t.Fatalf("FetchDailyReport() error: %v", err)
	}
	if !strings.Contains(report, "DeFi Llama USDC/USDT Yields Report") {
		t.Error("report missing header")
	}
	if !strings.Contains(report, "Aave V3") {
		t.Error("report missing Aave V3")
	}
	if !strings.Contains(report, "Venus") {
		t.Error("report missing Venus")
	}
	if !strings.Contains(report, "✅ Immediate") {
		t.Error("report missing immediate withdrawal indicator")
	}
	if !strings.Contains(report, "⏱️ 7d") {
		t.Error("report missing 7d withdrawal indicator")
	}
	if !strings.Contains(report, "Total: 2 USDC/USDT pools tracked") {
		t.Error("report missing pool count")
	}
}

func TestDefiLlama_FetchDailyReport_Empty(t *testing.T) {
	d := NewDefiLlama(nil)
	_, err := d.FetchDailyReport()
	if err == nil {
		t.Fatal("expected error for empty pools")
	}
}

func TestDefiLlamaPool_WithdrawalDays_WordBoundary(t *testing.T) {
	// Regression: "100daily" should not match as "100d"
	tests := []struct {
		name     string
		poolMeta *string
		want     int
	}{
		{"100daily should not match", strPtr("100daily rewards"), 0},
		{"100d withdrawal should match", strPtr("100d withdrawal"), 100},
		{"5d at end of string", strPtr("5d"), 5},
		{"10d followed by space", strPtr("10d lockup"), 10},
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
