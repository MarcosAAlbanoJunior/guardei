// Package bot contém a lógica de conversa (máquina de estados) e a ligação com o Telegram.
package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/marcosjunior/guardei/internal/ai"
	"github.com/marcosjunior/guardei/internal/platform"
	"github.com/marcosjunior/guardei/internal/search"
	"github.com/marcosjunior/guardei/internal/store"
)

const (
	reasonUpdate = "atualizar:" // pending de atualização de item existente: "atualizar:<id>"
	recentLimit  = 10
	maxSnippet   = 90
	aiTimeout    = 30 * time.Second
)

// Handler processa mensagens já desacopladas do Telegram, o que permite testar o fluxo.
type Handler struct {
	Store       *store.Store
	AI          ai.AIClient
	Searcher    *search.Searcher
	AIInfo      string // texto do /status, ex.: nomes dos modelos
	Allowed     map[int64]bool
	SearchLimit int
	Reply       func(ctx context.Context, chatID int64, text string)
}

// Handle trata uma mensagem de texto de um usuário.
func (h *Handler) Handle(ctx context.Context, userID, chatID int64, text string) {
	if !h.Allowed[userID] {
		slog.Warn("usuário não autorizado", "user_id", userID)
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	var err error
	switch {
	case strings.HasPrefix(text, "/"):
		err = h.command(ctx, userID, chatID, text)
	default:
		if link, rest, ok := platform.FindURL(text); ok {
			err = h.link(ctx, userID, chatID, link, rest)
		} else if p, perr := h.Store.GetPending(ctx, chatID); perr == nil {
			err = h.describe(ctx, userID, chatID, p, text)
		} else {
			err = h.search(ctx, userID, chatID, text)
		}
	}
	if err != nil {
		slog.Error("erro ao tratar mensagem", "err", err)
		h.Reply(ctx, chatID, "Algo deu errado do meu lado. Tente de novo em instantes.")
	}
}

func (h *Handler) command(ctx context.Context, userID, chatID int64, text string) error {
	cmd, args, _ := strings.Cut(text, " ")
	cmd, _, _ = strings.Cut(cmd, "@") // /cmd@nome_do_bot
	args = strings.TrimSpace(args)

	switch cmd {
	case "/start", "/help":
		h.Reply(ctx, chatID, helpText)
	case "/recentes":
		items, err := h.Store.Recent(ctx, userID, recentLimit)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			h.Reply(ctx, chatID, "Ainda não há itens salvos. Envie um link para começar.")
			return nil
		}
		h.Reply(ctx, chatID, formatItems("Últimos itens:", items))
	case "/editar":
		return h.edit(ctx, userID, chatID, args)
	case "/apagar":
		id, err := parseID(args)
		if err != nil {
			h.Reply(ctx, chatID, "Uso: /apagar <id>")
			return nil
		}
		switch err := h.Store.Delete(ctx, userID, id); {
		case errors.Is(err, store.ErrNotFound):
			h.Reply(ctx, chatID, fmt.Sprintf("Não achei o item #%d.", id))
		case err != nil:
			return err
		default:
			h.Searcher.Index.Remove(id)
			h.Reply(ctx, chatID, fmt.Sprintf("Item #%d apagado.", id))
		}
	case "/reindexar":
		return h.reindex(ctx, userID, chatID)
	case "/cancelar":
		ok, err := h.Store.ClearPending(ctx, chatID)
		if err != nil {
			return err
		}
		if ok {
			h.Reply(ctx, chatID, "Cancelado.")
		} else {
			h.Reply(ctx, chatID, "Não havia nada em espera.")
		}
	case "/status":
		n, err := h.Store.Count(ctx, userID)
		if err != nil {
			return err
		}
		if !h.AI.Enabled() {
			h.Reply(ctx, chatID, fmt.Sprintf("IA: desligada (modo manual)\nItens salvos: %d", n))
			return nil
		}
		emb, err := h.Store.CountEmbedded(ctx, userID)
		if err != nil {
			return err
		}
		h.Reply(ctx, chatID, fmt.Sprintf("IA: ligada (%s)\nItens salvos: %d\nCom embedding: %d", h.AIInfo, n, emb))
	default:
		h.Reply(ctx, chatID, "Comando desconhecido. Veja /help.")
	}
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
		h.Reply(ctx, chatID, fmt.Sprintf("Não achei o item #%d.", id))
		return nil
	}
	if err != nil {
		return err
	}
	if note = strings.TrimSpace(note); note != "" {
		return h.applyNote(ctx, userID, chatID, it.ID, note)
	}
	if err := h.Store.SetPending(ctx, store.Pending{ChatID: chatID, URL: it.URL, Reason: reasonUpdate + strconv.FormatInt(id, 10)}); err != nil {
		return err
	}
	h.Reply(ctx, chatID, fmt.Sprintf("Envie a nova descrição do item #%d (ou /cancelar).", id))
	return nil
}

func (h *Handler) applyNote(ctx context.Context, userID, chatID, id int64, note string) error {
	summary, tags := h.analyze(ctx, note)
	if err := h.Store.UpdateContent(ctx, userID, id, note, summary, tags); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			h.Reply(ctx, chatID, fmt.Sprintf("Não achei o item #%d.", id))
			return nil
		}
		return err
	}
	// O conteúdo mudou: o vetor antigo saiu do banco e sai do índice também.
	h.Searcher.Index.Remove(id)
	h.embed(ctx, userID, id, embedText(store.Item{UserNote: note, Summary: summary, Tags: tags}))
	h.Reply(ctx, chatID, fmt.Sprintf("Descrição do item #%d atualizada.", id))
	return nil
}

// analyze pede resumo e tags à IA. Sem IA ou com falha, devolve vazio: o item
// é salvo só com a descrição e o /reindexar completa depois.
func (h *Handler) analyze(ctx context.Context, note string) (string, []string) {
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
	h.Searcher.Index.Set(userID, id, v)
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

// link trata mensagem com link: salva na hora se veio com texto, senão pede descrição.
func (h *Handler) link(ctx context.Context, userID, chatID int64, raw, note string) error {
	plat, canonical, err := platform.Canonical(raw)
	if err != nil {
		h.Reply(ctx, chatID, "Não consegui entender esse link.")
		return err2nil(err)
	}
	// Novo link encerra qualquer espera anterior.
	if _, err := h.Store.ClearPending(ctx, chatID); err != nil {
		return err
	}

	existing, err := h.Store.FindByCanonical(ctx, userID, canonical)
	switch {
	case err == nil:
		if note != "" {
			return h.applyNote(ctx, userID, chatID, existing.ID, note)
		}
		if err := h.Store.SetPending(ctx, store.Pending{ChatID: chatID, URL: raw, Reason: reasonUpdate + strconv.FormatInt(existing.ID, 10)}); err != nil {
			return err
		}
		h.Reply(ctx, chatID, fmt.Sprintf("Esse link já está salvo (#%d). Envie uma nova descrição para atualizar, ou /cancelar.", existing.ID))
		return nil
	case !errors.Is(err, store.ErrNotFound):
		return err
	}

	if note != "" {
		return h.save(ctx, userID, chatID, raw, canonical, plat, note)
	}
	reason := h.manualReason(plat)
	if err := h.Store.SetPending(ctx, store.Pending{ChatID: chatID, URL: raw, Reason: reason}); err != nil {
		return err
	}
	h.Reply(ctx, chatID, fmt.Sprintf("Não vou transcrever esse vídeo: %s.\nMe diga do que ele trata e eu guardo (ou /cancelar).", reason))
	return nil
}

func (h *Handler) manualReason(plat string) string {
	switch {
	case !platform.Extract[plat]:
		return "esta plataforma não tem extração de áudio"
	case !h.AI.Enabled():
		return "a IA não está ligada (sem GEMINI_API_KEY)"
	}
	return "a extração de áudio ainda não está disponível"
}

// describe trata a resposta do usuário a um pedido de descrição.
func (h *Handler) describe(ctx context.Context, userID, chatID int64, p store.Pending, text string) error {
	defer h.Store.ClearPending(ctx, chatID)
	if idStr, ok := strings.CutPrefix(p.Reason, reasonUpdate); ok {
		id, err := parseID(idStr)
		if err != nil {
			return err
		}
		return h.applyNote(ctx, userID, chatID, id, text)
	}
	plat, canonical, err := platform.Canonical(p.URL)
	if err != nil {
		return err
	}
	return h.save(ctx, userID, chatID, p.URL, canonical, plat, text)
}

func (h *Handler) save(ctx context.Context, userID, chatID int64, raw, canonical, plat, note string) error {
	summary, tags := h.analyze(ctx, note)
	it := store.Item{
		UserID: userID, URL: raw, CanonicalURL: canonical, Platform: plat,
		UserNote: note, Summary: summary, Tags: tags, Source: "manual",
	}
	id, err := h.Store.Insert(ctx, &it)
	if err != nil {
		return err
	}
	h.embed(ctx, userID, id, embedText(it))

	msg := fmt.Sprintf("Salvo (#%d, %s):\n%s", id, plat, snippet(note))
	if summary != "" {
		msg = fmt.Sprintf("Salvo (#%d, %s):\n%s", id, plat, snippet(summary))
		if len(tags) > 0 {
			msg += "\nTags: " + strings.Join(tags, ", ")
		}
	}
	h.Reply(ctx, chatID, msg)
	return nil
}

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

func formatItems(title string, items []store.Item) string {
	var b strings.Builder
	b.WriteString(title)
	for _, it := range items {
		fmt.Fprintf(&b, "\n\n#%d · %s\n%s\n%s", it.ID, it.Platform, snippet(itemText(it)), it.URL)
	}
	return b.String()
}

func itemText(it store.Item) string {
	for _, s := range []string{it.Title, it.Summary, it.UserNote} {
		if s != "" {
			return s
		}
	}
	return "(sem descrição)"
}

func snippet(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > maxSnippet {
		return string(r[:maxSnippet]) + "…"
	}
	return s
}

func parseID(s string) (int64, error) {
	return strconv.ParseInt(strings.TrimPrefix(strings.TrimSpace(s), "#"), 10, 64)
}

// err2nil descarta um erro já tratado com resposta ao usuário.
func err2nil(error) error { return nil }

const helpText = `Guardei: salve links de vídeos e ache depois.

• Envie um link (com uma descrição na mesma mensagem, se quiser) para salvar.
• Envie qualquer texto sem link para buscar.

/recentes — últimos itens
/editar <id> [texto] — troca a descrição
/apagar <id> — remove um item
/reindexar — gera resumo e embeddings pendentes
/cancelar — cancela a espera por descrição
/status — estado do bot`
