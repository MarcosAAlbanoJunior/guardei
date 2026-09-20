package bot

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/MarcosAAlbanoJunior/guardei/internal/store"
)

func (h *Handler) command(ctx context.Context, userID, chatID int64, text string) error {
	cmd, args, _ := strings.Cut(text, " ")
	cmd, _, _ = strings.Cut(cmd, "@") // /cmd@nome_do_bot
	args = strings.TrimSpace(args)

	switch cmd {
	case "/start", "/help":
		h.Reply(ctx, chatID, helpText)
		return nil
	case "/recentes":
		return h.recent(ctx, userID, chatID)
	case "/editar":
		return h.edit(ctx, userID, chatID, args)
	case "/apagar":
		return h.remove(ctx, userID, chatID, args)
	case "/reindexar":
		return h.reindex(ctx, userID, chatID)
	case "/cancelar":
		return h.cancel(ctx, chatID)
	case "/status":
		return h.status(ctx, userID, chatID)
	}
	h.Reply(ctx, chatID, "Comando desconhecido. Veja /help.")
	return nil
}

func (h *Handler) recent(ctx context.Context, userID, chatID int64) error {
	items, err := h.Store.Recent(ctx, userID, recentLimit)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		h.Reply(ctx, chatID, "Ainda não há itens salvos. Envie um link para começar.")
		return nil
	}
	h.Reply(ctx, chatID, formatItems("Últimos itens:", items))
	return nil
}

// edit: "/editar <id> <texto>" troca na hora; "/editar <id>" pede o texto na próxima mensagem.
func (h *Handler) edit(ctx context.Context, userID, chatID int64, args string) error {
	idStr, note, _ := strings.Cut(args, " ")
	id, err := parseID(idStr)
	if err != nil {
		h.Reply(ctx, chatID, "Uso: /editar <id> [nova descrição]")
		return nil
	}
	it, err := h.Store.Get(ctx, userID, id)
	if errors.Is(err, store.ErrNotFound) {
		h.itemNotFound(ctx, chatID, id)
		return nil
	}
	if err != nil {
		return err
	}
	if note = strings.TrimSpace(note); note != "" {
		return h.applyNote(ctx, userID, chatID, it.ID, note)
	}
	if err := h.Store.SetPending(ctx, store.Pending{ChatID: chatID, URL: it.URL, ItemID: it.ID}); err != nil {
		return err
	}
	h.Reply(ctx, chatID, fmt.Sprintf("Envie a nova descrição do item #%d (ou /cancelar).", id))
	return nil
}

func (h *Handler) remove(ctx context.Context, userID, chatID int64, args string) error {
	id, err := parseID(args)
	if err != nil {
		h.Reply(ctx, chatID, "Uso: /apagar <id>")
		return nil
	}
	switch err := h.Store.Delete(ctx, userID, id); {
	case errors.Is(err, store.ErrNotFound):
		h.itemNotFound(ctx, chatID, id)
	case err != nil:
		return err
	default:
		h.Searcher.Forget(id)
		h.Reply(ctx, chatID, fmt.Sprintf("Item #%d apagado.", id))
	}
	return nil
}

func (h *Handler) cancel(ctx context.Context, chatID int64) error {
	ok, err := h.Store.ClearPending(ctx, chatID)
	if err != nil {
		return err
	}
	if ok {
		h.Reply(ctx, chatID, "Cancelado.")
	} else {
		h.Reply(ctx, chatID, "Não havia nada em espera.")
	}
	return nil
}

func (h *Handler) status(ctx context.Context, userID, chatID int64) error {
	n, err := h.Store.Count(ctx, userID)
	if err != nil {
		return err
	}
	if !h.AI.Enabled() {
		h.Reply(ctx, chatID, fmt.Sprintf("IA: desligada (modo manual)\nItens salvos: %d", n))
		return nil
	}
	embedded, err := h.Store.CountEmbedded(ctx, userID)
	if err != nil {
		return err
	}
	h.Reply(ctx, chatID, fmt.Sprintf("IA: ligada (%s)\nItens salvos: %d\nCom embedding: %d", h.AIInfo, n, embedded))
	return nil
}
