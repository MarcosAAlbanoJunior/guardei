package bot

import (
	"context"
	"log/slog"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// Run conecta o Handler ao Telegram por long polling e bloqueia até ctx terminar.
func Run(ctx context.Context, token string, h *Handler) error {
	b, err := tg.New(token, tg.WithDefaultHandler(func(ctx context.Context, b *tg.Bot, u *models.Update) {
		switch {
		case u.CallbackQuery != nil:
			onCallback(ctx, b, h, u.CallbackQuery)
		case u.Message != nil && u.Message.From != nil && u.Message.Text != "":
			h.Handle(ctx, u.Message.From.ID, u.Message.Chat.ID, u.Message.Text)
		}
	}))
	if err != nil {
		return err
	}

	noPreview := true
	send := func(ctx context.Context, chatID int64, text string, markup models.ReplyMarkup) {
		_, err := b.SendMessage(ctx, &tg.SendMessageParams{
			ChatID:             chatID,
			Text:               text,
			LinkPreviewOptions: &models.LinkPreviewOptions{IsDisabled: &noPreview},
			ReplyMarkup:        markup,
		})
		if err != nil {
			slog.Error("falha ao enviar mensagem", "err", err)
		}
	}
	h.Reply = func(ctx context.Context, chatID int64, text string) { send(ctx, chatID, text, nil) }
	h.Keyboard = func(ctx context.Context, chatID int64, text string, rows [][]Button) {
		send(ctx, chatID, text, toMarkup(rows))
	}
	h.ClearKeyboard = func(ctx context.Context, chatID int64, messageID int) {
		_, err := b.EditMessageReplyMarkup(ctx, &tg.EditMessageReplyMarkupParams{
			ChatID:      chatID,
			MessageID:   messageID,
			ReplyMarkup: &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{}},
		})
		if err != nil { // mensagem muito antiga ou já sem botões: não afeta o fluxo
			slog.Debug("não consegui tirar os botões", "err", err)
		}
	}
	b.Start(ctx)
	return nil
}

// onCallback responde ao toque (tira o "carregando" do botão) e repassa ao Handler.
func onCallback(ctx context.Context, b *tg.Bot, h *Handler, cq *models.CallbackQuery) {
	if _, err := b.AnswerCallbackQuery(ctx, &tg.AnswerCallbackQueryParams{CallbackQueryID: cq.ID}); err != nil {
		slog.Debug("falha ao responder o toque", "err", err)
	}
	msg := cq.Message.Message
	if msg == nil { // mensagem inacessível (muito antiga): nada a fazer
		return
	}
	h.HandleCallback(ctx, cq.From.ID, msg.Chat.ID, msg.ID, cq.Data)
}

// toMarkup converte linhas de botões no teclado inline do Telegram.
func toMarkup(rows [][]Button) *models.InlineKeyboardMarkup {
	kb := make([][]models.InlineKeyboardButton, len(rows))
	for i, row := range rows {
		kb[i] = make([]models.InlineKeyboardButton, len(row))
		for j, btn := range row {
			kb[i][j] = models.InlineKeyboardButton{Text: btn.Label, CallbackData: btn.Data}
		}
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: kb}
}
