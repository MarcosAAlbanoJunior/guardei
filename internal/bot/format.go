package bot

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/MarcosAAlbanoJunior/guardei/internal/search"
	"github.com/MarcosAAlbanoJunior/guardei/internal/store"
)

const (
	maxSnippet = 90  // linha de um item em listas de resultados
	maxSummary = 300 // resumo na confirmação de "Salvo"
	maxTitle   = 80  // título guardado de um post

	// maxMessage é o maior texto que o bot monta. O Telegram recusa mais de 4096
	// caracteres; a folga cobre o que conta em dobro (emojis) e o cabeçalho.
	maxMessage = 3800
)

const helpText = `Guardei: salve vídeos e posts e ache depois por busca.

• Envie um link (com uma descrição na mesma mensagem, se quiser) para salvar.
• Envie qualquer texto sem link para buscar. Dá para dizer "hoje", "essa semana", "mês passado" ou "do youtube" na busca.

/recentes [termo] — itens do mais novo ao mais antigo
/editar <id> [texto] — troca a descrição
/apagar <id> — remove um item
/reindexar — gera resumo e embeddings pendentes
/cancelar — cancela a espera por descrição
/status — estado do bot`

// savedMessage monta a confirmação de um item salvo. kind é "" (descrição
// manual), "transcrito" ou "post". body é o resumo, ou a descrição sem IA.
func savedMessage(id int64, plat, kind, title, body string, tags []string) string {
	head := fmt.Sprintf("Salvo (#%d, %s", id, plat)
	if kind != "" {
		head += ", " + kind
	}
	lines := []string{head + "):"}
	if title != "" {
		lines = append(lines, shorten(title, maxSnippet))
	}
	lines = append(lines, shorten(body, maxSummary))
	if len(tags) > 0 {
		lines = append(lines, "Tags: "+strings.Join(tags, ", "))
	}
	return strings.Join(lines, "\n")
}

// pageHeader descreve a página que está sendo mostrada (itens from a to, 1-based).
func pageHeader(sess *searchSession, from, to int) string {
	total := len(sess.ranking.IDs)
	count := fmt.Sprint(total)
	if total >= search.MaxRanked {
		count += "+" // a lista é cortada em MaxRanked
	}
	byDate := sess.query.Text == "" || sess.newest

	var head string
	switch {
	case total == 1 && byDate:
		head = "1 item:"
	case total == 1:
		head = "Achei 1 item:"
	case from > 1:
		head = fmt.Sprintf("Resultados %d–%d de %s:", from, to, count)
	case byDate:
		head = fmt.Sprintf("%s itens, do mais novo ao mais antigo · mostrando %s:", count, span(from, to))
	default:
		head = fmt.Sprintf("Achei %s itens · mostrando %s:", count, span(from, to))
	}
	if label := sess.query.Label(); label != "" {
		head += "\nFiltro: " + label
	}
	return head
}

func span(from, to int) string {
	if from == to {
		return fmt.Sprint(from)
	}
	return fmt.Sprintf("%d–%d", from, to)
}

// itemBlock é o trecho de um item na lista: id, plataforma, idade, texto e link.
func itemBlock(it store.Item, now time.Time) string {
	return fmt.Sprintf("\n\n#%d · %s · %s\n%s\n%s", it.ID, it.Platform, age(it.CreatedAt, now), shorten(itemText(it), maxSnippet), it.URL)
}

// fitCount diz quantos dos primeiros itens cabem em uma mensagem (pelo menos
// um, se houver). Links longos podem fazer 10 itens passarem do limite do Telegram.
func fitCount(items []store.Item, now time.Time) int {
	used := 300 // reserva para o cabeçalho
	for i, it := range items {
		used += len([]rune(itemBlock(it, now)))
		if used > maxMessage && i > 0 {
			return i
		}
	}
	return len(items)
}

// formatItems junta o cabeçalho e os itens; nunca passa de maxMessage.
func formatItems(header string, items []store.Item, now time.Time) string {
	var b strings.Builder
	b.WriteString(header)
	for _, it := range items {
		b.WriteString(itemBlock(it, now))
	}
	if r := []rune(b.String()); len(r) > maxMessage { // um só item com link gigante
		return string(r[:maxMessage-1]) + "…"
	}
	return b.String()
}

// age diz há quanto tempo o item foi salvo, em dias de calendário: "hoje", "ontem", "há 3 dias"…
func age(t, now time.Time) string {
	day := func(x time.Time) time.Time {
		x = x.In(now.Location())
		return time.Date(x.Year(), x.Month(), x.Day(), 0, 0, 0, 0, now.Location())
	}
	days := int(day(now).Sub(day(t)).Hours()/24 + 0.5)
	switch {
	case days <= 0:
		return "hoje"
	case days == 1:
		return "ontem"
	case days < 14:
		return fmt.Sprintf("há %d dias", days)
	case days < 60:
		return fmt.Sprintf("há %d semanas", days/7)
	}
	return fmt.Sprintf("há %d meses", days/30)
}

func itemText(it store.Item) string {
	for _, s := range []string{it.Title, it.Summary, it.UserNote} {
		if s != "" {
			return s
		}
	}
	return "(sem descrição)"
}

// shorten junta os espaços e corta em n caracteres, terminando em "…" se cortou.
func shorten(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func parseID(s string) (int64, error) {
	return strconv.ParseInt(strings.TrimPrefix(strings.TrimSpace(s), "#"), 10, 64)
}
