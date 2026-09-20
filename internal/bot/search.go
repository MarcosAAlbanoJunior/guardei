package bot

import (
	"context"
	"fmt"

	"github.com/MarcosAAlbanoJunior/guardei/internal/search"
	"github.com/MarcosAAlbanoJunior/guardei/internal/store"
)

// search trata uma mensagem sem link como busca nos itens do usuário.
func (h *Handler) search(ctx context.Context, userID, chatID int64, query string) error {
	results, err := h.Searcher.Search(ctx, userID, query, h.SearchLimit)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		h.Reply(ctx, chatID, "Não achei nada. Tente termos mais gerais.")
		return nil
	}
	results = search.Pick(results)
	items := make([]store.Item, len(results))
	for i, r := range results {
		items[i] = r.Item
	}
	title := "Achei:"
	if len(items) > 1 {
		title = fmt.Sprintf("Achei %d resultados:", len(items))
	}
	h.Reply(ctx, chatID, formatItems(title, items))
	return nil
}
