package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/marcosjunior/guardei/internal/ai"
	"github.com/marcosjunior/guardei/internal/page"
	"github.com/marcosjunior/guardei/internal/store"
)

const pageTimeout = 30 * time.Second

func (h *Handler) pages() page.Reader {
	if h.Pages == nil {
		return page.Nop{}
	}
	return h.Pages
}

// readPost salva o link lendo o texto público do post e pedindo à IA título,
// resumo e tags. Devolve "" quando salvou, ou o motivo de precisar pedir a descrição.
func (h *Handler) readPost(ctx context.Context, userID, chatID int64, raw, canonical, plat string) (string, error) {
	h.Reply(ctx, chatID, "⏳ Lendo o post…")

	rctx, cancel := context.WithTimeout(ctx, pageTimeout)
	pg, err := h.pages().Read(rctx, raw)
	cancel()
	if err != nil {
		slog.Warn("leitura da página falhou", "url", raw, "err", err)
		switch {
		case errors.Is(err, page.ErrBlocked):
			return "o site bloqueou o acesso ou pede login", nil
		case errors.Is(err, page.ErrNoContent):
			return "a página não tem texto aproveitável", nil
		case errors.Is(err, page.ErrUnavailable):
			return "a leitura de páginas não está disponível", nil
		}
		return "não consegui abrir a página", nil
	}

	actx, cancel := context.WithTimeout(ctx, aiTimeout)
	defer cancel()
	a, err := h.AI.AnalyzePost(actx, ai.Post{URL: raw, Platform: plat, Title: pg.Title,
		Author: pg.Author, SiteName: pg.SiteName, Text: pg.Text})
	if err != nil {
		slog.Warn("análise do post por IA falhou", "url", raw, "err", err)
		return "a IA não conseguiu analisar o post", nil
	}
	if !a.HasContent {
		return "o que consegui ler não descreve o post (tela de login ou bloqueio?)", nil
	}

	title := a.Title
	if title == "" {
		title = pg.Title
	}
	// O texto do post vai na coluna transcript: é o "texto extraído do conteúdo",
	// e já entra no índice full-text.
	it := store.Item{
		UserID: userID, URL: raw, CanonicalURL: canonical, Platform: plat,
		Title: shorten(title, maxTitle), Transcript: pg.Text, Summary: a.Summary, Tags: a.Tags, Source: "page",
	}
	id, err := h.Store.Insert(ctx, &it)
	if err != nil {
		return "", err
	}
	h.embed(ctx, userID, id, embedText(it))

	msg := savedMessage(id, plat, "post", it.Title, it.Summary, it.Tags)
	if pg.Partial {
		msg += fmt.Sprintf("\n⚠️ O site só entregou o começo do texto. Use /editar %d para completar.", id)
	}
	h.Reply(ctx, chatID, msg)
	return "", nil
}
