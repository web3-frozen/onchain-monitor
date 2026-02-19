package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSubscribeValidation(t *testing.T) {
	// Subscribe requires a store, but we can test input validation
	// that returns before hitting the store.
	handler := Subscribe(nil)

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "invalid JSON",
			body:       `{invalid`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing tg_chat_id",
			body:       `{"event_id": 1}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing event_id",
			body:       `{"tg_chat_id": 123}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "zero tg_chat_id",
			body:       `{"tg_chat_id": 0, "event_id": 1}`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/subscriptions", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestListSubscriptionsMissingParam(t *testing.T) {
	handler := ListSubscriptions(nil)

	// Missing tg_chat_id
	req := httptest.NewRequest(http.MethodGet, "/api/subscriptions", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing param: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	// Invalid tg_chat_id
	req = httptest.NewRequest(http.MethodGet, "/api/subscriptions?tg_chat_id=abc", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid param: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
