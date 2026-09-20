package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/marcosjunior/guardei/internal/ai"
	"github.com/marcosjunior/guardei/internal/store"
)

// applyNote troca a descrição de um item, refaz resumo e tags e reindexa.
func (h *Handler) applyNote(ctx context.Context, userID, chatID, id int64, note string) error {
	summary, tags := h.analyze(ctx, note)
	if err := h.Store.UpdateContent(ctx, userID, id, note, summary, tags); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			h.itemNotFound(ctx, chatID, id)
			return nil
		}
		return err
	}
	// O conteúdo mudou: o vetor antigo saiu do banco e sai do índice também.
	h.Searcher.Forget(id)
	h.embed(ctx, userID, id, embedText(store.Item{UserNote: note, Summary: summary, Tags: tags}))
	h.Reply(ctx, chatID, fmt.Sprintf("Descrição do item #%d atualizada.", id))
	return nil
}

// analyze pede resumo e tags à IA. Sem IA ou com falha, devolve vazio: o item
// é salvo só com a descrição e o /reindexar completa depois.
func (h *Handler) analyze(ctx context.Context, note string) (summary string, tags []string) {
	if !h.AI.Enabled() {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(ctx, aiTimeout)
	defer cancel()
	a, err := h.AI.AnalyzeText(ctx, note)
	if err != nil {
		slog.Warn("análise por IA falhou; salvando só a descrição", "err", err)
		return "", nil
	}
	return a.Summary, a.Tags
}

// embed gera e guarda o vetor do item; falha é registrada e não interrompe o fluxo.
func (h *Handler) embed(ctx context.Context, userID, id int64, text string) bool {
	if !h.AI.Enabled() {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, aiTimeout)
	defer cancel()
	v, err := h.AI.Embed(ctx, text, ai.TaskDocument)
	if err == nil {
		err = h.Store.SetEmbedding(ctx, userID, id, v)
	}
	if err != nil {
		slog.Warn("embedding falhou; use /reindexar depois", "item", id, "err", err)
		return false
	}
	h.Searcher.Remember(userID, id, v)
	return true
}

// embedText junta o que descreve o item, do mais para o menos condensado.
func embedText(it store.Item) string {
	var parts []string
	for _, s := range []string{it.Title, it.Summary} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	if len(it.Tags) > 0 {
		parts = append(parts, "Tags: "+strings.Join(it.Tags, ", "))
	}
	if it.UserNote != "" {
		parts = append(parts, it.UserNote)
	}
	return strings.Join(parts, "\n")
}

// reindex completa resumo, tags e embedding dos itens salvos antes de a IA existir.
func (h *Handler) reindex(ctx context.Context, userID, chatID int64) error {
	if !h.AI.Enabled() {
		h.Reply(ctx, chatID, "A IA está desligada (sem GEMINI_API_KEY). Nada a reindexar.")
		return nil
	}
	items, err := h.Store.WithoutEmbedding(ctx, userID)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		h.Reply(ctx, chatID, "Todos os itens já têm embedding.")
		return nil
	}
	h.Reply(ctx, chatID, fmt.Sprintf("Reindexando %d itens…", len(items)))

	done := 0
	for _, it := range items {
		if it.Summary == "" && it.UserNote != "" {
			if summary, tags := h.analyze(ctx, it.UserNote); summary != "" {
				if err := h.Store.UpdateContent(ctx, userID, it.ID, it.UserNote, summary, tags); err != nil {
					return err
				}
				it.Summary, it.Tags = summary, tags
			}
		}
		if h.embed(ctx, userID, it.ID, embedText(it)) {
			done++
		}
	}
	msg := fmt.Sprintf("Reindexados %d de %d itens.", done, len(items))
	if done < len(items) {
		msg += " Os demais falharam; tente /reindexar de novo daqui a pouco."
	}
	h.Reply(ctx, chatID, msg)
	return nil
}
