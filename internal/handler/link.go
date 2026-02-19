package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/web3-frozen/onchain-monitor/internal/store"
)

// LinkStatus checks whether a Telegram chat ID is linked.
func LinkStatus(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		chatIDStr := r.URL.Query().Get("tg_chat_id")
		if chatIDStr == "" {
			http.Error(w, `{"error":"tg_chat_id required"}`, http.StatusBadRequest)
			return
		}
		chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid tg_chat_id"}`, http.StatusBadRequest)
			return
		}

		user, err := s.GetTelegramUser(r.Context(), chatID)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]bool{"linked": false})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"linked": user.Linked})
	}
}

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

func UnlinkTelegram(s *store.Store) http.HandlerFunc {
	type request struct {
		TgChatID int64 `json:"tg_chat_id"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		if req.TgChatID == 0 {
			http.Error(w, `{"error":"tg_chat_id required"}`, http.StatusBadRequest)
			return
		}

		if err := s.UnlinkTelegram(r.Context(), req.TgChatID); err != nil {
			http.Error(w, `{"error":"failed to unlink"}`, http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
