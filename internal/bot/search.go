package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/MarcosAAlbanoJunior/guardei/internal/search"
	"github.com/MarcosAAlbanoJunior/guardei/internal/store"
)

// Button é um botão de resposta rápida sob uma mensagem. Data volta ao bot quando ele é tocado.
type Button struct{ Label, Data string }

type searchOpts struct {
	pageSize int
	newest   bool
	qvec     []float32 // embedding já calculado para o mesmo texto
}

// search trata uma mensagem sem link como busca nos itens do usuário. Palavras
// como "hoje", "essa semana" ou "do youtube" viram filtros.
func (h *Handler) search(ctx context.Context, userID, chatID int64, raw string) error {
	return h.startSearch(ctx, userID, chatID, search.ParseQuery(raw, time.Now()), searchOpts{pageSize: h.SearchLimit})
}

// startSearch ordena os resultados (só ids), guarda a lista na sessão do chat e mostra a primeira página.
func (h *Handler) startSearch(ctx context.Context, userID, chatID int64, q search.Query, o searchOpts) error {
	rk, err := h.Searcher.Rank(ctx, userID, q, search.RankOpts{QueryVec: o.qvec, Newest: o.newest})
	if err != nil {
		return err
	}
	sess := &searchSession{userID: userID, query: q, newest: o.newest, pageSize: o.pageSize, ranking: rk}
	h.sessions.put(chatID, sess)
	return h.showPage(ctx, chatID, sess)
}

// showPage mostra a próxima página da sessão, lendo do banco só os itens dela.
func (h *Handler) showPage(ctx context.Context, chatID int64, sess *searchSession) error {
	ids := sess.ranking.IDs
	if len(ids) == 0 {
		h.sendWithButtons(ctx, chatID, emptyResultText(sess.query), h.buttons(sess))
		return nil
	}
	if sess.shown >= len(ids) {
		h.Reply(ctx, chatID, "Não há mais resultados.")
		return nil
	}

	n := sess.pageSize
	if sess.shown == 0 && search.Dominant(sess.ranking.Scores) {
		n = 1 // o primeiro vale bem mais que o segundo: mostra só ele; "Ver mais" traz o resto
	}
	from := sess.shown
	var items []store.Item
	for from < len(ids) && len(items) == 0 { // pula ids apagados desde a busca
		to := min(from+n, len(ids))
		var err error
		if items, err = h.Store.GetMany(ctx, sess.userID, ids[from:to]); err != nil {
			return err
		}
		if len(items) == 0 {
			from = to
		}
	}
	if len(items) == 0 {
		sess.shown = len(ids)
		h.Reply(ctx, chatID, "Não há mais resultados.")
		return nil
	}

	// A página só leva os itens que cabem numa mensagem; o resto fica para o "Ver mais".
	now := time.Now()
	items = items[:fitCount(items, now)]
	first := from
	sess.shown = indexOf(ids, items[len(items)-1].ID) + 1

	header := pageHeader(sess, first+1, sess.shown)
	h.sendWithButtons(ctx, chatID, formatItems(header, items, now), h.buttons(sess))
	return nil
}

func indexOf(ids []int64, id int64) int {
	for i, x := range ids {
		if x == id {
			return i
		}
	}
	return len(ids) - 1
}

func emptyResultText(q search.Query) string {
	switch {
	case q.Text == "" && q.Filter.IsZero():
		return "Ainda não há itens salvos. Envie um link para começar."
	case q.Label() != "":
		return fmt.Sprintf("Não achei nada (filtro: %s). Tente sem o filtro ou com termos mais gerais.", q.Label())
	}
	return "Não achei nada. Tente termos mais gerais."
}

// buttons monta os botões da página atual: "Ver mais" e os de período.
func (h *Handler) buttons(sess *searchSession) [][]Button {
	total, remaining := len(sess.ranking.IDs), len(sess.ranking.IDs)-sess.shown
	var rows [][]Button
	if remaining > 0 {
		rows = append(rows, []Button{{Label: fmt.Sprintf("Ver mais %d ▶", min(sess.pageSize, remaining)), Data: fmt.Sprintf("m:%d", sess.id)}})
	}
	switch {
	case sess.query.HasPeriod():
		rows = append(rows, []Button{{Label: "Todo o período", Data: fmt.Sprintf("p:%d:0", sess.id)}})
	case total > sess.pageSize:
		rows = append(rows, []Button{
			{Label: "Hoje", Data: fmt.Sprintf("p:%d:1", sess.id)},
			{Label: "7 dias", Data: fmt.Sprintf("p:%d:7", sess.id)},
			{Label: "30 dias", Data: fmt.Sprintf("p:%d:30", sess.id)},
		})
	}
	return rows
}

// sendWithButtons envia o texto com os botões, ou só o texto se não houver botões.
func (h *Handler) sendWithButtons(ctx context.Context, chatID int64, text string, rows [][]Button) {
	if len(rows) == 0 || h.Keyboard == nil {
		h.Reply(ctx, chatID, text)
		return
	}
	h.Keyboard(ctx, chatID, text, rows)
}

func (h *Handler) clearKeyboard(ctx context.Context, chatID int64, messageID int) {
	if h.ClearKeyboard != nil && messageID != 0 {
		h.ClearKeyboard(ctx, chatID, messageID)
	}
}

// HandleCallback trata o toque em um botão ("Ver mais", período) de uma busca.
func (h *Handler) HandleCallback(ctx context.Context, userID, chatID int64, messageID int, data string) {
	if !h.Allowed[userID] {
		return
	}
	defer h.lockChat(chatID)()

	if err := h.callback(ctx, chatID, messageID, data); err != nil {
		h.fail(ctx, chatID, err)
	}
}

func (h *Handler) callback(ctx context.Context, chatID int64, messageID int, data string) error {
	kind, rest, _ := strings.Cut(data, ":")
	sidStr, arg, _ := strings.Cut(rest, ":")
	sid, err := strconv.ParseInt(sidStr, 10, 64)
	if err != nil {
		return nil // botão de outra versão: ignora
	}
	sess := h.sessions.get(chatID, sid)
	if sess == nil {
		h.clearKeyboard(ctx, chatID, messageID)
		h.Reply(ctx, chatID, "Essa busca expirou. Envie a busca de novo.")
		return nil
	}
	if sess.handled[messageID] { // toque duplo no mesmo botão
		return nil
	}
	sess.handled[messageID] = true
	h.clearKeyboard(ctx, chatID, messageID)

	switch kind {
	case "m":
		return h.showPage(ctx, chatID, sess)
	case "p":
		days, err := strconv.Atoi(arg)
		if err != nil {
			return nil
		}
		// Mesmo texto, outro período: reaproveita o embedding, sem nova chamada à IA.
		q := sess.query.WithPeriod(days, time.Now())
		return h.startSearch(ctx, sess.userID, chatID, q, searchOpts{pageSize: sess.pageSize, newest: sess.newest, qvec: sess.ranking.QueryVec})
	}
	return nil
}
