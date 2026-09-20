package search

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/MarcosAAlbanoJunior/guardei/internal/store"
)

// Query é uma busca já interpretada: o texto a procurar e os filtros de período
// e plataforma que o usuário escreveu junto ("receitas da semana", "só do youtube").
type Query struct {
	Text   string
	Filter store.Filter

	period, platform string // rótulos legíveis dos filtros
}

// Label descreve os filtros ativos, por exemplo "YouTube · últimos 7 dias".
func (q Query) Label() string {
	var parts []string
	for _, p := range []string{q.platform, q.period} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " · ")
}

// HasPeriod diz se há filtro de período.
func (q Query) HasPeriod() bool { return !q.Filter.Since.IsZero() || !q.Filter.Until.IsZero() }

// WithPeriod troca o filtro de período: 1 = hoje, 7 e 30 = últimos N dias, 0 = todo o período.
func (q Query) WithPeriod(days int, now time.Time) Query {
	q.Filter.Since, q.Filter.Until, q.period = time.Time{}, time.Time{}, ""
	switch days {
	case 1:
		q.setPeriod(startOfDay(now), time.Time{}, "hoje")
	case 7, 30:
		q.setPeriod(now.AddDate(0, 0, -days), time.Time{}, fmt.Sprintf("últimos %d dias", days))
	}
	return q
}

func (q *Query) setPeriod(since, until time.Time, label string) {
	q.Filter.Since, q.Filter.Until, q.period = since, until, label
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

var (
	// prefixos que acompanham "semana" e "mês" nas formas "essa semana", "do mês"…
	thisPrefix = map[string]bool{"essa": true, "esta": true, "dessa": true, "desta": true, "nessa": true, "nesta": true,
		"esse": true, "este": true, "desse": true, "deste": true, "nesse": true, "neste": true,
		"da": true, "na": true, "do": true, "no": true}
	pastPrefix = map[string]bool{"da": true, "na": true, "do": true, "no": true, "de": true, "a": true, "o": true}

	// dayPrefix são as palavras que, antes de "hoje"/"ontem", mostram que é um filtro de data.
	dayPrefix = map[string]bool{"de": true, "do": true, "da": true, "desde": true, "salvei": true, "salvo": true,
		"salvos": true, "salvas": true, "guardei": true, "adicionei": true, "vi": true}

	platformWords = map[string]string{"youtube": "youtube", "tiktok": "tiktok", "instagram": "instagram", "insta": "instagram",
		"linkedin": "linkedin", "twitter": "x"}
	platformLabel = map[string]string{"youtube": "YouTube", "tiktok": "TikTok", "instagram": "Instagram", "linkedin": "LinkedIn", "x": "X"}

	// connectors antecedem o nome de uma plataforma ("só do youtube") e somem com ele.
	connectors = map[string]bool{"do": true, "da": true, "no": true, "na": true, "pelo": true, "pela": true,
		"de": true, "em": true, "so": true, "apenas": true, "somente": true}
	// fillers são palavras de pedido ("o que eu salvei hoje") que, sozinhas, não dizem o que procurar.
	fillers = map[string]bool{"o": true, "os": true, "a": true, "as": true, "que": true, "eu": true, "salvei": true,
		"salvo": true, "salvos": true, "salvas": true, "tudo": true, "meus": true, "minhas": true, "videos": true,
		"vi": true, "posts": true, "post": true, "links": true, "link": true, "mostre": true, "mostra": true, "me": true,
		"traga": true, "quero": true, "ver": true, "lista": true, "listar": true, "dos": true, "das": true, "um": true, "uma": true}
)

// ParseQuery separa da mensagem os filtros de período ("hoje", "ontem", "essa
// semana", "semana passada", "mês passado", "últimos 15 dias") e de plataforma
// ("do youtube", "no tiktok", "só instagram", "no x"). O que sobra é o texto da
// busca; se sobrar só palavras de pedido, o texto fica vazio e a busca lista os
// itens do filtro do mais novo ao mais antigo.
func ParseQuery(raw string, now time.Time) Query {
	words := strings.Fields(raw)
	n := len(words)
	fw := make([]string, n) // sem acento, minúsculas e sem pontuação nas pontas
	for i, w := range words {
		fw[i] = fold(strings.Trim(w, `.,;:!?()"'`))
	}
	used := make([]bool, n)
	mark := func(idx ...int) {
		for _, i := range idx {
			used[i] = true
		}
	}
	prev := func(i int) string {
		if i > 0 && !used[i-1] {
			return fw[i-1]
		}
		return ""
	}
	next := func(i int) string {
		if i+1 < n && !used[i+1] {
			return fw[i+1]
		}
		return ""
	}

	q := Query{Text: strings.TrimSpace(raw)}
	found := false // achou algum filtro

	for i := 0; i < n && q.Filter.Platform == ""; i++ {
		if used[i] {
			continue
		}
		p, ok := platformWords[fw[i]]
		if !ok && fw[i] == "x" && connectors[prev(i)] {
			p, ok = "x", true
		}
		if !ok {
			continue
		}
		q.Filter.Platform, q.platform = p, platformLabel[p]
		mark(i)
		for j, c := i-1, 0; j >= 0 && c < 2 && !used[j] && connectors[fw[j]]; j, c = j-1, c+1 {
			mark(j)
		}
		found = true
	}

	for i := 0; i < n && q.period == ""; i++ {
		if used[i] {
			continue
		}
		switch fw[i] {
		case "hoje":
			if dayIsFilter(fw, used, i) {
				q.setPeriod(startOfDay(now), time.Time{}, "hoje")
				mark(i)
			}
		case "ontem":
			if dayIsFilter(fw, used, i) {
				q.setPeriod(startOfDay(now).AddDate(0, 0, -1), startOfDay(now), "ontem")
				mark(i)
			}
		case "semana":
			switch {
			case next(i) == "passada":
				q.setPeriod(now.AddDate(0, 0, -14), now.AddDate(0, 0, -7), "semana passada")
				mark(i, i+1)
				if pastPrefix[prev(i)] {
					mark(i - 1)
				}
			case thisPrefix[prev(i)] || prev(i) == "ultima":
				q = q.WithPeriod(7, now)
				mark(i, i-1)
			}
		case "mes":
			switch {
			case next(i) == "passado":
				q.setPeriod(now.AddDate(0, 0, -60), now.AddDate(0, 0, -30), "mês passado")
				mark(i, i+1)
				if pastPrefix[prev(i)] {
					mark(i - 1)
				}
			case thisPrefix[prev(i)] || prev(i) == "ultimo":
				q = q.WithPeriod(30, now)
				mark(i, i-1)
			}
		case "ultimos", "ultimas":
			if days, ok := lastN(fw, i); ok {
				q.setPeriod(now.AddDate(0, 0, -days), time.Time{}, fmt.Sprintf("últimos %d dias", days))
				mark(i, i+1, i+2)
			}
		}
	}
	found = found || q.period != ""

	if !found {
		return q
	}
	q.Text = leftover(words, fw, used)
	return q
}

// dayIsFilter decide se "hoje"/"ontem" na posição i é um filtro de data ou parte
// do assunto ("algo para cozinhar hoje"). É filtro quando abre a busca, vem depois
// de "de"/"salvei"…, encosta em outro filtro ("só youtube hoje") ou a busca não
// tem outro assunto ("o que eu salvei hoje").
func dayIsFilter(fw []string, used []bool, i int) bool {
	if i == 0 || used[i-1] || (i+1 < len(fw) && used[i+1]) || dayPrefix[fw[i-1]] {
		return true
	}
	for j, w := range fw {
		if j != i && !used[j] && !(connectors[w] || fillers[w] || stopwords[w]) {
			return false
		}
	}
	return true
}

// lastN lê "últimos N dias|semanas|meses" a partir de i.
func lastN(fw []string, i int) (days int, ok bool) {
	if i+2 >= len(fw) {
		return 0, false
	}
	n, err := strconv.Atoi(fw[i+1])
	if err != nil || n < 1 || n > 365 {
		return 0, false
	}
	switch fw[i+2] {
	case "dia", "dias":
		return n, true
	case "semana", "semanas":
		return n * 7, true
	case "mes", "meses":
		return n * 30, true
	}
	return 0, false
}

// leftover monta o texto da busca com o que não virou filtro, sem palavras de
// pedido ou conectores nas pontas; vazio se só restarem elas.
func leftover(words, fw []string, used []bool) string {
	var w, f []string
	for i := range words {
		if !used[i] {
			w, f = append(w, words[i]), append(f, fw[i])
		}
	}
	noise := func(x string) bool { return connectors[x] || fillers[x] || stopwords[x] }
	for len(f) > 0 && noise(f[0]) {
		w, f = w[1:], f[1:]
	}
	for len(f) > 0 && noise(f[len(f)-1]) {
		w, f = w[:len(w)-1], f[:len(f)-1]
	}
	return strings.Join(w, " ")
}

var accents = strings.NewReplacer("á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a", "é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i", "ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u", "ç", "c")

func fold(s string) string { return accents.Replace(strings.ToLower(s)) }
