package monitor

import (
	"context"
	"fmt"

	"github.com/web3-frozen/onchain-monitor/internal/metrics"
)

// AlphaAirdrop represents an airdrop event from the Alpha endpoint.
type AlphaAirdrop struct {
	Token  string
	Date   string
	Time   string
	Points int
	Name   string
}

// checkAlphaAirdrops queries the registered alpha source for current airdrops
// and sends Telegram alerts to subscribers of the "general_alpha_alert" event.
func (e *Engine) checkAlphaAirdrops(ctx context.Context) {
	alphaSrc, ok := e.sources["alpha"]
	if !ok {
		return
	}

	type airdropGetter interface {
		GetAirdrops() []AlphaAirdrop
	}

	getter, ok := alphaSrc.(airdropGetter)
	if !ok {
		return
	}

	airdrops := getter.GetAirdrops()
	if len(airdrops) == 0 {
		return
	}

	chatIDs, err := e.store.GetSubscriberChatIDs(ctx, "general_alpha_alert")
	if err != nil || len(chatIDs) == 0 {
		return
	}

	for _, ad := range airdrops {
		for _, chatID := range chatIDs {
			dedupKey := fmt.Sprintf("alpha:%d:%s:%s:%s", chatID, ad.Token, ad.Date, ad.Time)
			if e.dedup.AlreadySent(ctx, dedupKey) {
				continue
			}

			msg := fmt.Sprintf("🎉 New Alpha Airdrop: %s\n\nDate: %s %s\nPoints: %d\nName: %s\n",
				ad.Token, ad.Date, ad.Time, ad.Points, ad.Name)

			if err := e.alertFn(chatID, msg); err != nil {
				metrics.AlertsFailedTotal.WithLabelValues("alpha", "alpha_airdrop").Inc()
				e.logger.Error("send alpha alert failed", "chat_id", chatID, "error", err)
				continue
			}
			metrics.AlertsSentTotal.WithLabelValues("alpha", "alpha_airdrop").Inc()
			e.logNotification(chatID, "alpha", "general_alpha_alert",
				fmt.Sprintf("%s on %s %s (%d points)", ad.Token, ad.Date, ad.Time, ad.Points))
			e.dedup.Record(ctx, dedupKey)
		}
	}
}
