package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/web3-frozen/onchain-monitor/internal/dedup"
	"github.com/web3-frozen/onchain-monitor/internal/store"
)

func ListSubscriptions(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tgChatIDStr := r.URL.Query().Get("tg_chat_id")
		if tgChatIDStr == "" {
			http.Error(w, `{"error":"tg_chat_id required"}`, http.StatusBadRequest)
			return
		}

		tgChatID, err := strconv.ParseInt(tgChatIDStr, 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid tg_chat_id"}`, http.StatusBadRequest)
			return
		}

		subs, err := s.ListSubscriptions(r.Context(), tgChatID)
		if err != nil {
			http.Error(w, `{"error":"failed to list subscriptions"}`, http.StatusInternalServerError)
			return
		}
		if subs == nil {
			subs = []store.Subscription{}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(subs)
	}
}

func Subscribe(s *store.Store) http.HandlerFunc {
	type request struct {
		TgChatID       int64   `json:"tg_chat_id"`
		EventID        int     `json:"event_id"`
		ThresholdPct   float64 `json:"threshold_pct"`
		WindowMinutes  int     `json:"window_minutes"`
		Direction      string  `json:"direction"`
		ReportHour     *int    `json:"report_hour"`
		ThresholdValue float64 `json:"threshold_value"`
		Coin           string  `json:"coin"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		if req.TgChatID == 0 || req.EventID == 0 {
			http.Error(w, `{"error":"tg_chat_id and event_id required"}`, http.StatusBadRequest)
			return
		}

		if req.ThresholdPct <= 0 {
			req.ThresholdPct = 10
		}
		if req.WindowMinutes <= 0 {
			req.WindowMinutes = 1
		}
		validDirs := map[string]bool{"drop": true, "increase": true, "decrease": true, "higher": true, "lower": true, "long": true, "short": true, "stablecoin": true, "non-stablecoin": true, "any": true}
		if !validDirs[req.Direction] {
			req.Direction = "drop"
		}
		reportHour := 8
		if req.ReportHour != nil && *req.ReportHour >= 0 && *req.ReportHour <= 23 {
			reportHour = *req.ReportHour
		}
		if req.ThresholdValue < 0 {
			req.ThresholdValue = 0
		}

		sub, err := s.Subscribe(r.Context(), req.TgChatID, req.EventID, req.ThresholdPct, req.WindowMinutes, req.Direction, reportHour, req.ThresholdValue, req.Coin)
		if err != nil {
			http.Error(w, `{"error":"failed to subscribe"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(sub)
	}
}

func UpdateSubscription(s *store.Store) http.HandlerFunc {
	type request struct {
		ThresholdPct   float64 `json:"threshold_pct"`
		WindowMinutes  int     `json:"window_minutes"`
		Direction      string  `json:"direction"`
		ReportHour     *int    `json:"report_hour"`
		ThresholdValue float64 `json:"threshold_value"`
		Coin           string  `json:"coin"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid subscription id"}`, http.StatusBadRequest)
			return
		}

		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		if req.ThresholdPct <= 0 {
			req.ThresholdPct = 10
		}
		if req.WindowMinutes <= 0 {
			req.WindowMinutes = 1
		}
		validDirs := map[string]bool{"drop": true, "increase": true, "decrease": true, "higher": true, "lower": true, "long": true, "short": true, "stablecoin": true, "non-stablecoin": true, "any": true}
		if !validDirs[req.Direction] {
			req.Direction = "drop"
		}
		reportHour := 8
		if req.ReportHour != nil && *req.ReportHour >= 0 && *req.ReportHour <= 23 {
			reportHour = *req.ReportHour
		}
		if req.ThresholdValue < 0 {
			req.ThresholdValue = 0
		}

		sub, err := s.UpdateSubscription(r.Context(), id, req.ThresholdPct, req.WindowMinutes, req.Direction, reportHour, req.ThresholdValue, req.Coin)
		if err != nil {
			http.Error(w, `{"error":"failed to update subscription"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sub)
	}
}

func Unsubscribe(s *store.Store, d *dedup.Deduplicator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid subscription id"}`, http.StatusBadRequest)
			return
		}

		// Get chat_id before deleting so we can clear dedup keys
		chatID, _ := s.GetSubscriptionChatID(r.Context(), id)

		if err := s.Unsubscribe(r.Context(), id); err != nil {
			http.Error(w, `{"error":"failed to unsubscribe"}`, http.StatusInternalServerError)
			return
		}

		// Clear all dedup keys for this user so re-subscribing starts fresh
		if chatID != 0 && d != nil {
			d.ClearByPattern(r.Context(), fmt.Sprintf("*%d:*", chatID))
			d.ClearByPattern(r.Context(), fmt.Sprintf("merkl:%d:*", chatID))
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
