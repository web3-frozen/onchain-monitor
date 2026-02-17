package handler

import (
	"encoding/json"
	"net/http"

	"github.com/web3-frozen/onchain-monitor/internal/store"
)

func LinkTelegram(s *store.Store) http.HandlerFunc {
	type request struct {
		Code string `json:"code"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		if req.Code == "" {
			http.Error(w, `{"error":"code required"}`, http.StatusBadRequest)
			return
		}

		user, err := s.LinkByCode(r.Context(), req.Code)
		if err != nil {
			http.Error(w, `{"error":"invalid or expired link code"}`, http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(user)
	}
}
