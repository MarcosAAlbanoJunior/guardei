package bot

import (
	"context"
	"log/slog"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// Run conecta o Handler ao Telegram por long polling e bloqueia até ctx terminar.
func Run(ctx context.Context, token string, h *Handler) error {
	b, err := tg.New(token, tg.WithDefaultHandler(func(ctx context.Context, _ *tg.Bot, u *models.Update) {
		if u.Message == nil || u.Message.From == nil || u.Message.Text == "" {
			return
		}
		h.Handle(ctx, u.Message.From.ID, u.Message.Chat.ID, u.Message.Text)
	}))
	if err != nil {
		return err
	}
	h.Reply = func(ctx context.Context, chatID int64, text string) {
		off := true
		_, err := b.SendMessage(ctx, &tg.SendMessageParams{
			ChatID:             chatID,
			Text:               text,
			LinkPreviewOptions: &models.LinkPreviewOptions{IsDisabled: &off},
		})
		if err != nil {
			slog.Error("falha ao enviar mensagem", "err", err)
		}
	}
	b.Start(ctx)
	return nil
}
