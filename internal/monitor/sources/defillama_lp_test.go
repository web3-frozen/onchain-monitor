package sources

import (
	"testing"
)

func TestIsLPPool(t *testing.T) {
	tests := []struct {
		name     string
		pool     DefiLlamaPool
		expected bool
	}{
		{
			name:     "multi exposure pool",
			pool:     DefiLlamaPool{Symbol: "WETH-USDC", Exposure: "multi"},
			expected: true,
		},
		{
			name:     "single exposure with dash symbol",
			pool:     DefiLlamaPool{Symbol: "SUI-USDC", Exposure: "single"},
			expected: true,
		},
		{
			name:     "single exposure single asset",
			pool:     DefiLlamaPool{Symbol: "STETH", Exposure: "single"},
			expected: false,
		},
		{
			name:     "empty exposure with LP symbol",
			pool:     DefiLlamaPool{Symbol: "ETH-DAI", Exposure: ""},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isLPPool(tt.pool)
			if got != tt.expected {
				t.Errorf("isLPPool(%+v) = %v, want %v", tt.pool, got, tt.expected)
			}
		})
	}
}

func TestFilterLPPools(t *testing.T) {
	reward5 := 5.0
	reward10 := 10.0
	reward2 := 2.0
	base1 := 1.0

	pools := []DefiLlamaPool{
		{
			Chain: "Sui", Project: "cetus", Symbol: "SUI-USDC",
			TVLUsd: 5_000_000, APY: 15, APYBase: &base1, APYReward: &reward10,
			Exposure: "multi", Pool: "pool-1",
		},
		{
			Chain: "Ethereum", Project: "uniswap-v3", Symbol: "WETH-USDC",
			TVLUsd: 50_000_000, APY: 6, APYBase: &base1, APYReward: &reward5,
			Exposure: "multi", Pool: "pool-2",
		},
		{
			Chain: "Sui", Project: "turbos", Symbol: "SUI-USDT",
			TVLUsd: 2_000_000, APY: 3, APYBase: &base1, APYReward: &reward2,
			Exposure: "multi", Pool: "pool-3",
		},
		// Single asset - should be filtered out
		{
			Chain: "Sui", Project: "lido", Symbol: "STETH",
			TVLUsd: 10_000_000, APY: 3, APYBase: &base1, APYReward: &reward2,
			Exposure: "single", Pool: "pool-4",
		},
		// Outlier — still included (DeFi Llama outlier is statistical, not a legitimacy flag)
		{
			Chain: "Sui", Project: "scam-dex", Symbol: "SCAM-SUI",
			TVLUsd: 1_000_000, APY: 500, APYBase: &base1, APYReward: &reward10,
			Exposure: "multi", Outlier: true, Pool: "pool-5",
		},
	}

	d := NewDefiLlamaLP(nil)

	t.Run("filter by chain Sui with 3% min reward", func(t *testing.T) {
		result := d.FilterLPPools(pools, 3, 1_000_000, "Sui")
		if len(result) != 2 {
			t.Fatalf("expected 2 pools, got %d", len(result))
		}
		// pool-1 (reward=10) and pool-5 (reward=10, outlier), sorted desc
		if result[0].Pool != "pool-1" && result[0].Pool != "pool-5" {
			t.Errorf("expected pool-1 or pool-5 first, got %s", result[0].Pool)
		}
	})

	t.Run("filter all chains with 3% min reward", func(t *testing.T) {
		result := d.FilterLPPools(pools, 3, 1_000_000, "ALL")
		if len(result) != 3 {
			t.Fatalf("expected 3 pools, got %d", len(result))
		}
		// Should be sorted by reward APY descending: pool-1(10), pool-5(10), pool-2(5)
		if result[2].Pool != "pool-2" {
			t.Errorf("expected pool-2 last (lowest reward), got %s", result[2].Pool)
		}
	})

	t.Run("TVL filter excludes small pools", func(t *testing.T) {
		result := d.FilterLPPools(pools, 1, 10_000_000, "ALL")
		if len(result) != 1 {
			t.Fatalf("expected 1 pool, got %d", len(result))
		}
		if result[0].Pool != "pool-2" {
			t.Errorf("expected pool-2, got %s", result[0].Pool)
		}
	})

	t.Run("case-insensitive chain filter", func(t *testing.T) {
		result := d.FilterLPPools(pools, 1, 100_000, "sui")
		if len(result) != 3 {
			t.Fatalf("expected 3 Sui pools, got %d", len(result))
		}
	})

	t.Run("no reward APY returns empty", func(t *testing.T) {
		noRewardPools := []DefiLlamaPool{
			{
				Chain: "Sui", Project: "test", Symbol: "A-B",
				TVLUsd: 5_000_000, APY: 5, APYBase: &base1, APYReward: nil,
				Exposure: "multi", Pool: "pool-no-reward",
			},
		}
		result := d.FilterLPPools(noRewardPools, 1, 100_000, "ALL")
		if len(result) != 0 {
			t.Fatalf("expected 0 pools (nil reward), got %d", len(result))
		}
	})
}
