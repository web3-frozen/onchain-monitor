package sources

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTurtleOpportunity_TotalYield(t *testing.T) {
	tests := []struct {
		name       string
		incentives []struct {
			Name  string  `json:"name"`
			Yield float64 `json:"yield"`
		}
		want float64
	}{
		{"no incentives", nil, 0},
		{"single", []struct {
			Name  string  `json:"name"`
			Yield float64 `json:"yield"`
		}{{"Lending", 5.5}}, 5.5},
		{"multiple", []struct {
			Name  string  `json:"name"`
			Yield float64 `json:"yield"`
		}{{"Lending", 5.0}, {"Boost", 3.2}}, 8.2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := TurtleOpportunity{Incentives: tt.incentives}
			if got := o.TotalYield(); got != tt.want {
				t.Errorf("TotalYield() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTurtleOpportunity_ChainName(t *testing.T) {
	o := TurtleOpportunity{}
	if got := o.ChainName(); got != "Unknown" {
		t.Errorf("empty ChainName() = %q, want Unknown", got)
	}
	o.DepositTokens = append(o.DepositTokens, struct {
		Symbol string  `json:"symbol"`
		Price  float64 `json:"priceUsd"`
		Chain  struct {
			Name string `json:"name"`
			Slug string `json:"slug"`
		} `json:"chain"`
	}{Symbol: "USDC", Chain: struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}{Name: "Ethereum"}})
	if got := o.ChainName(); got != "Ethereum" {
		t.Errorf("ChainName() = %q, want Ethereum", got)
	}
}

func TestTurtleOpportunity_IsStablecoin(t *testing.T) {
	tests := []struct {
		name   string
		tokens []struct {
			Symbol string  `json:"symbol"`
			Price  float64 `json:"priceUsd"`
			Chain  struct {
				Name string `json:"name"`
				Slug string `json:"slug"`
			} `json:"chain"`
		}
		want bool
	}{
		{"no tokens", nil, false},
		{"USDC", []struct {
			Symbol string  `json:"symbol"`
			Price  float64 `json:"priceUsd"`
			Chain  struct {
				Name string `json:"name"`
				Slug string `json:"slug"`
			} `json:"chain"`
		}{{Symbol: "USDC", Price: 1.0}}, true},
		{"ETH", []struct {
			Symbol string  `json:"symbol"`
			Price  float64 `json:"priceUsd"`
			Chain  struct {
				Name string `json:"name"`
				Slug string `json:"slug"`
			} `json:"chain"`
		}{{Symbol: "ETH", Price: 3000}}, false},
		{"price near $1", []struct {
			Symbol string  `json:"symbol"`
			Price  float64 `json:"priceUsd"`
			Chain  struct {
				Name string `json:"name"`
				Slug string `json:"slug"`
			} `json:"chain"`
		}{{Symbol: "UNKNOWN_STABLE", Price: 0.999}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := TurtleOpportunity{DepositTokens: tt.tokens}
			if got := o.IsStablecoin(); got != tt.want {
				t.Errorf("IsStablecoin() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFilterOpportunities(t *testing.T) {
	opps := []TurtleOpportunity{
		{Status: "active", TVL: 1000000, Incentives: []struct {
			Name  string  `json:"name"`
			Yield float64 `json:"yield"`
		}{{"A", 10}}},
		{Status: "draft", TVL: 2000000, Incentives: []struct {
			Name  string  `json:"name"`
			Yield float64 `json:"yield"`
		}{{"B", 20}}},
		{Status: "active", TVL: 100, Incentives: []struct {
			Name  string  `json:"name"`
			Yield float64 `json:"yield"`
		}{{"C", 15}}},
		{Status: "active", TVL: 2000000, Incentives: []struct {
			Name  string  `json:"name"`
			Yield float64 `json:"yield"`
		}{{"D", 3}}},
	}

	result := filterOpportunities(opps, 5, 500000)
	if len(result) != 1 {
		t.Fatalf("got %d, want 1", len(result))
	}
	if result[0].TVL != 1000000 {
		t.Errorf("wrong opp filtered, TVL = %v", result[0].TVL)
	}
}

func TestTurtle_FetchSnapshot(t *testing.T) {
	data := turtleResponse{
		Opportunities: []TurtleOpportunity{
			{
				Name:   "Test Vault",
				Status: "active",
				TVL:    1000000,
				Incentives: []struct {
					Name  string  `json:"name"`
					Yield float64 `json:"yield"`
				}{{"Lending", 12.5}},
			},
			{
				Name:   "Small Vault",
				Status: "active",
				TVL:    100,
				Incentives: []struct {
					Name  string  `json:"name"`
					Yield float64 `json:"yield"`
				}{{"Lending", 20}},
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(data)
	}))
	defer srv.Close()

	turtle := NewTurtle(nil)
	turtle.baseURL = srv.URL

	snap, err := turtle.FetchSnapshot()
	if err != nil {
		t.Fatalf("FetchSnapshot() error: %v", err)
	}
	if snap.Source != "turtle" {
		t.Errorf("Source = %q, want turtle", snap.Source)
	}
	// Only 1 opportunity passes filter (TVL >= 500K, yield >= 5%)
	if snap.Metrics["opportunities"] != 1 {
		t.Errorf("opportunities = %v, want 1", snap.Metrics["opportunities"])
	}
	if snap.Metrics["top_yield"] != 12.5 {
		t.Errorf("top_yield = %v, want 12.5", snap.Metrics["top_yield"])
	}
}

func TestTurtle_OrganizationName(t *testing.T) {
	o := TurtleOpportunity{}
	if got := o.OrganizationName(); got != "Unknown" {
		t.Errorf("empty = %q, want Unknown", got)
	}

	o.Products = append(o.Products, struct {
		Name         string `json:"name"`
		Organization struct {
			Name string `json:"name"`
		} `json:"organization"`
	}{Organization: struct {
		Name string `json:"name"`
	}{Name: "Morpho"}})
	if got := o.OrganizationName(); got != "Morpho" {
		t.Errorf("= %q, want Morpho", got)
	}
}
