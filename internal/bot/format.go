package bot

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/marcosjunior/guardei/internal/store"
)

const (
	maxSnippet = 90  // linha de um item em listas de resultados
	maxSummary = 300 // resumo na confirmação de "Salvo"
	maxTitle   = 80  // título guardado de um post
)

const helpText = `Guardei: salve vídeos e posts e ache depois por busca.

• Envie um link (com uma descrição na mesma mensagem, se quiser) para salvar.
• Envie qualquer texto sem link para buscar.

/recentes — últimos itens
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

func formatItems(title string, items []store.Item) string {
	var b strings.Builder
	b.WriteString(title)
	for _, it := range items {
		fmt.Fprintf(&b, "\n\n#%d · %s\n%s\n%s", it.ID, it.Platform, shorten(itemText(it), maxSnippet), it.URL)
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
