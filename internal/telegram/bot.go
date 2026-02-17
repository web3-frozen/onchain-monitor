package telegram

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/web3-frozen/onchain-monitor/internal/store"
)

const telegramAPI = "https://api.telegram.org/bot"

type Bot struct {
	token  string
	store  *store.Store
	logger *slog.Logger
	client *http.Client
	offset int64
}

func NewBot(token string, s *store.Store, logger *slog.Logger) *Bot {
	return &Bot{
		token:  token,
		store:  s,
		logger: logger,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// SendMessage sends a text message to a Telegram chat.
func (b *Bot) SendMessage(chatID int64, text string) error {
	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "HTML",
	}
	body, _ := json.Marshal(payload)

	resp, err := b.client.Post(
		telegramAPI+b.token+"/sendMessage",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("send message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Description string `json:"description"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		return fmt.Errorf("telegram API error %d: %s", resp.StatusCode, errResp.Description)
	}
	return nil
}

// Run starts the long-polling loop for incoming Telegram messages.
func (b *Bot) Run(ctx context.Context) {
	b.logger.Info("telegram bot started")
	for {
		select {
		case <-ctx.Done():
			return
		default:
			b.poll(ctx)
		}
	}
}

func (b *Bot) poll(ctx context.Context) {
	url := fmt.Sprintf("%s%s/getUpdates?offset=%d&timeout=30", telegramAPI, b.token, b.offset)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		b.logger.Error("create poll request", "error", err)
		return
	}

	resp, err := b.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		b.logger.Error("poll updates", "error", err)
		time.Sleep(5 * time.Second)
		return
	}
	defer resp.Body.Close()

	var result struct {
		OK     bool `json:"ok"`
		Result []struct {
			UpdateID int64 `json:"update_id"`
			Message  *struct {
				Chat struct {
					ID int64 `json:"id"`
				} `json:"chat"`
				From struct {
					Username string `json:"username"`
				} `json:"from"`
				Text string `json:"text"`
			} `json:"message"`
		} `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		b.logger.Error("decode updates", "error", err)
		return
	}

	for _, u := range result.Result {
		b.offset = u.UpdateID + 1
		if u.Message == nil {
			continue
		}

		chatID := u.Message.Chat.ID
		text := strings.TrimSpace(u.Message.Text)
		username := u.Message.From.Username

		switch {
		case text == "/start":
			b.handleStart(ctx, chatID, username)
		case text == "/help":
			b.handleHelp(chatID)
		case text == "/status":
			b.handleStatus(ctx, chatID)
		default:
			_ = b.SendMessage(chatID, "Unknown command. Send /help for available commands.")
		}
	}
}

func (b *Bot) handleStart(ctx context.Context, chatID int64, username string) {
	code := generateLinkCode()
	expiresAt := time.Now().Add(10 * time.Minute)

	if err := b.store.UpsertTelegramUser(ctx, chatID, username, code, expiresAt); err != nil {
		b.logger.Error("upsert telegram user", "error", err)
		_ = b.SendMessage(chatID, "❌ Error generating link code. Please try again.")
		return
	}

	msg := fmt.Sprintf("👋 Welcome to Onchain Monitor!\n\n"+
		"Your link code: <code>%s</code>\n\n"+
		"Enter this code on the monitoring dashboard to link your Telegram account and subscribe to alerts.\n\n"+
		"⏰ This code expires in 10 minutes.", code)
	_ = b.SendMessage(chatID, msg)
}

func (b *Bot) handleHelp(chatID int64) {
	msg := "🤖 <b>Onchain Monitor Bot</b>\n\n" +
		"Commands:\n" +
		"/start — Get a link code to connect your Telegram\n" +
		"/status — Check your subscription status\n" +
		"/help — Show this message\n\n" +
		"Manage subscriptions on the web dashboard."
	_ = b.SendMessage(chatID, msg)
}

func (b *Bot) handleStatus(ctx context.Context, chatID int64) {
	user, err := b.store.GetTelegramUser(ctx, chatID)
	if err != nil {
		_ = b.SendMessage(chatID, "You haven't linked your account yet. Send /start to get a link code.")
		return
	}

	if !user.Linked {
		_ = b.SendMessage(chatID, "Your account is registered but not linked yet. Send /start to get a new link code.")
		return
	}

	subs, err := b.store.ListSubscriptions(ctx, chatID)
	if err != nil {
		_ = b.SendMessage(chatID, "Error fetching subscriptions.")
		return
	}

	if len(subs) == 0 {
		_ = b.SendMessage(chatID, "✅ Account linked!\n\nYou have no active subscriptions. Visit the dashboard to subscribe to alerts.")
		return
	}

	events, _ := b.store.ListEvents(ctx)
	eventMap := make(map[int]string)
	for _, e := range events {
		eventMap[e.ID] = e.Description
	}

	msg := fmt.Sprintf("✅ Account linked! (@%s)\n\n📋 Active subscriptions:\n", user.TgUsername)
	for _, sub := range subs {
		desc := eventMap[sub.EventID]
		if desc == "" {
			desc = "Unknown event"
		}
		msg += fmt.Sprintf("• %s\n", desc)
	}
	_ = b.SendMessage(chatID, msg)
}

func generateLinkCode() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return strings.ToUpper(hex.EncodeToString(b))
}
