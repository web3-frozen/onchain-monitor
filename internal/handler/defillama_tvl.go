package handler

import (
	"encoding/json"
	"net/http"

	"github.com/web3-frozen/onchain-monitor/internal/monitor/sources"
)

type protocolSearchResult struct {
	Name     string   `json:"name"`
	Slug     string   `json:"slug"`
	TVL      float64  `json:"tvl"`
	Logo     string   `json:"logo"`
	Category string   `json:"category"`
	Chains   []string `json:"chains"`
}

type protocolSearcher interface {
	SearchProtocols(query string, limit int) []sources.DefiLlamaProtocol
	TopProtocols(limit int) []sources.DefiLlamaProtocol
}

// SearchDefiLlamaProtocols returns an HTTP handler that searches DeFiLlama protocols.
// GET /api/defillama/protocols/search?q=aave&limit=20
// When q is empty, returns top protocols by TVL.
func SearchDefiLlamaProtocols(searcher protocolSearcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		limit := 20

		var protocols []sources.DefiLlamaProtocol
		if query == "" {
			protocols = searcher.TopProtocols(limit)
		} else {
			protocols = searcher.SearchProtocols(query, limit)
		}

		results := make([]protocolSearchResult, 0, len(protocols))
		for _, p := range protocols {
			results = append(results, protocolSearchResult{
				Name:     p.Name,
				Slug:     p.Slug,
				TVL:      p.TVL,
				Logo:     p.Logo,
				Category: p.Category,
				Chains:   p.Chains,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(results)
	}
}
