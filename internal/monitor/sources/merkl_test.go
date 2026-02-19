package sources

import "testing"

func TestIsStablecoin(t *testing.T) {
	tests := []struct {
		name   string
		opp    MerklOpportunity
		want   bool
	}{
		{
			name: "USDC/USDT pair",
			opp: MerklOpportunity{
				Tokens: []struct {
					Symbol string  `json:"symbol"`
					Price  float64 `json:"price"`
				}{
					{Symbol: "USDC", Price: 1.0},
					{Symbol: "USDT", Price: 1.0},
				},
			},
			want: true,
		},
		{
			name: "single stablecoin",
			opp: MerklOpportunity{
				Tokens: []struct {
					Symbol string  `json:"symbol"`
					Price  float64 `json:"price"`
				}{
					{Symbol: "DAI", Price: 1.0},
				},
			},
			want: true,
		},
		{
			name: "BTC is not stablecoin",
			opp: MerklOpportunity{
				Tokens: []struct {
					Symbol string  `json:"symbol"`
					Price  float64 `json:"price"`
				}{
					{Symbol: "BTC", Price: 95000},
				},
			},
			want: false,
		},
		{
			name: "mixed stable and non-stable",
			opp: MerklOpportunity{
				Tokens: []struct {
					Symbol string  `json:"symbol"`
					Price  float64 `json:"price"`
				}{
					{Symbol: "USDC", Price: 1.0},
					{Symbol: "ETH", Price: 3500},
				},
			},
			want: false,
		},
		{
			name: "empty tokens",
			opp:  MerklOpportunity{},
			want: false,
		},
		{
			name: "unknown token near $1 (price fallback)",
			opp: MerklOpportunity{
				Tokens: []struct {
					Symbol string  `json:"symbol"`
					Price  float64 `json:"price"`
				}{
					{Symbol: "UNKNOWN", Price: 0.99},
				},
			},
			want: true,
		},
		{
			name: "price just outside range",
			opp: MerklOpportunity{
				Tokens: []struct {
					Symbol string  `json:"symbol"`
					Price  float64 `json:"price"`
				}{
					{Symbol: "UNKNOWN", Price: 1.06},
				},
			},
			want: false,
		},
		{
			name: "lowercase symbol still matches",
			opp: MerklOpportunity{
				Tokens: []struct {
					Symbol string  `json:"symbol"`
					Price  float64 `json:"price"`
				}{
					{Symbol: "usdc", Price: 1.0},
				},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.opp.IsStablecoin()
			if got != tt.want {
				t.Errorf("IsStablecoin() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProtocolName(t *testing.T) {
	tests := []struct {
		name string
		opp  MerklOpportunity
		want string
	}{
		{
			name: "with protocol",
			opp:  MerklOpportunity{Protocol: &struct{ Name string `json:"name"` }{Name: "Aave"}},
			want: "Aave",
		},
		{
			name: "nil protocol",
			opp:  MerklOpportunity{Protocol: nil},
			want: "Unknown",
		},
		{
			name: "empty protocol name",
			opp:  MerklOpportunity{Protocol: &struct{ Name string `json:"name"` }{Name: ""}},
			want: "Unknown",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.opp.ProtocolName()
			if got != tt.want {
				t.Errorf("ProtocolName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMerklURL(t *testing.T) {
	opp := MerklOpportunity{
		Type:       "LEND",
		Identifier: "0xabc123",
		Chain: struct {
			Name string `json:"name"`
		}{Name: "Arbitrum One"},
	}
	want := "https://app.merkl.xyz/opportunities/arbitrum-one/LEND/0xabc123"
	got := opp.MerklURL()
	if got != want {
		t.Errorf("MerklURL() = %q, want %q", got, want)
	}
}

func TestFmtTVL(t *testing.T) {
	tests := []struct {
		input float64
		want  string
	}{
		{500, "500"},
		{1500, "2K"},
		{50000, "50K"},
		{1000000, "1.0M"},
		{2500000, "2.5M"},
		{123456789, "123.5M"},
	}
	for _, tt := range tests {
		got := fmtTVL(tt.input)
		if got != tt.want {
			t.Errorf("fmtTVL(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
