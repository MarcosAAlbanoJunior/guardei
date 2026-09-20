// Package search combina FTS5 e busca vetorial para ordenar os itens de um
// usuário. O resultado é uma lista de ids: os dados de cada item só são lidos
// do banco quando uma página é mostrada.
package search

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/MarcosAAlbanoJunior/guardei/internal/ai"
	"github.com/MarcosAAlbanoJunior/guardei/internal/store"
)

const (
	// MinCosine é o piso de similaridade da busca por sentido. Medido com o
	// gemini-embedding-2: relevantes ficam em 0,67–0,79 e irrelevantes até 0,62.
	MinCosine = 0.65

	// relativeMargin descarta vizinhos mais de 0,07 abaixo do melhor: numa busca
	// específica só ficam os que se aproximam do melhor, e numa genérica (todos
	// próximos entre si) fica o conjunto inteiro. Calibrado com receitas x outros
	// assuntos; ajuste se os seus dados pedirem.
	relativeMargin = 0.07

	// MaxRanked é o tamanho máximo da lista ordenada de uma busca.
	MaxRanked = 100

	rrfK         = 60 // constante clássica do reciprocal rank fusion
	embedTimeout = 15 * time.Second

	// singleWinnerRatio: o primeiro resultado sozinho, se valer 1,5x o segundo.
	singleWinnerRatio = 1.5
)

// Searcher ordena os itens de um usuário: full-text sempre, e híbrida (RRF com
// a busca vetorial) quando há IA e vetores.
type Searcher struct {
	Store *store.Store
	AI    ai.Client
	Index *Index
}

// Ranking é o resultado ordenado de uma busca.
type Ranking struct {
	IDs []int64
	// Scores acompanha IDs, maior é melhor. Vazio quando a ordem é por data.
	Scores []float64
	// QueryVec é o embedding da consulta, para trocar o filtro sem nova chamada à IA.
	QueryVec []float32
}

// RankOpts ajusta uma busca.
type RankOpts struct {
	QueryVec []float32 // embedding já calculado para o mesmo texto
	Newest   bool      // ordena do mais novo ao mais antigo em vez de por relevância
}

// Remember guarda o vetor de um item no índice em memória.
func (s *Searcher) Remember(userID, id int64, v []float32) { s.Index.Set(userID, id, v) }

// Forget tira um item do índice em memória (apagado ou com vetor desatualizado).
func (s *Searcher) Forget(id int64) { s.Index.Remove(id) }

// Rank ordena os itens do usuário que casam com a consulta e o filtro, até
// MaxRanked. Sem texto na consulta, lista os itens do filtro do mais novo ao
// mais antigo. Falha no embedding da consulta cai no FTS5, sem erro.
func (s *Searcher) Rank(ctx context.Context, userID int64, q Query, o RankOpts) (Ranking, error) {
	text := strings.TrimSpace(q.Text)
	if text == "" {
		ids, err := s.Store.IDsByDate(ctx, userID, q.Filter, MaxRanked)
		return Ranking{IDs: ids}, err
	}

	var fts []store.ScoredID
	if match := BuildFTSQuery(text); match != "" {
		var err error
		if fts, err = s.Store.SearchFTSIDs(ctx, userID, match, q.Filter, MaxRanked); err != nil {
			return Ranking{}, err
		}
	}

	qvec, near, err := s.semantic(ctx, userID, text, q.Filter, o.QueryVec)
	if err != nil {
		return Ranking{}, err
	}
	rk := fuse(fts, near)
	rk.QueryVec = qvec

	if o.Newest {
		if rk.IDs, err = s.Store.OrderByNewest(ctx, userID, rk.IDs); err != nil {
			return Ranking{}, err
		}
		rk.Scores = nil
	}
	return rk, nil
}

// semantic devolve os vizinhos do texto no índice, respeitando o filtro. Sem IA
// ou sem vetores, devolve vazio.
func (s *Searcher) semantic(ctx context.Context, userID int64, text string, f store.Filter, qvec []float32) ([]float32, []Neighbor, error) {
	if s.AI == nil || !s.AI.Enabled() || s.Index == nil || s.Index.Len() == 0 {
		return qvec, nil, nil
	}
	if qvec == nil {
		ectx, cancel := context.WithTimeout(ctx, embedTimeout)
		defer cancel()
		v, err := s.AI.Embed(ectx, text, ai.TaskQuery)
		if err != nil {
			slog.Warn("embedding da consulta falhou; usando só FTS", "err", err)
			return nil, nil, nil
		}
		qvec = v
	}
	allow, err := s.Store.FilteredIDs(ctx, userID, f)
	if err != nil {
		return nil, nil, err
	}
	near := s.Index.Nearest(userID, qvec, MaxRanked, MinCosine, allow)
	return qvec, withinMargin(near), nil
}

// withinMargin mantém só os vizinhos a até relativeMargin do melhor (a lista vem ordenada).
func withinMargin(ns []Neighbor) []Neighbor {
	if len(ns) == 0 {
		return ns
	}
	floor := ns[0].Cosine - relativeMargin
	for i, n := range ns {
		if n.Cosine < floor {
			return ns[:i]
		}
	}
	return ns
}

// fuse ordena por relevância: só pelo bm25 quando não há vizinhos, senão pelo
// reciprocal rank fusion dos dois rankings.
func fuse(fts []store.ScoredID, near []Neighbor) Ranking {
	if len(near) == 0 {
		rk := Ranking{IDs: make([]int64, len(fts)), Scores: make([]float64, len(fts))}
		for i, h := range fts {
			rk.IDs[i], rk.Scores[i] = h.ID, -h.BM25 // bm25 é negativo: inverte para "maior é melhor"
		}
		return rk
	}
	score := map[int64]float64{}
	for i, h := range fts {
		score[h.ID] += 1.0 / float64(rrfK+i+1)
	}
	for i, n := range near {
		score[n.ID] += 1.0 / float64(rrfK+i+1)
	}
	ids := make([]int64, 0, len(score))
	for id := range score {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if score[ids[i]] != score[ids[j]] {
			return score[ids[i]] > score[ids[j]]
		}
		return ids[i] < ids[j]
	})
	if len(ids) > MaxRanked {
		ids = ids[:MaxRanked]
	}
	rk := Ranking{IDs: ids, Scores: make([]float64, len(ids))}
	for i, id := range ids {
		rk.Scores[i] = score[id]
	}
	return rk
}

// Dominant diz se o primeiro resultado vale bem mais que o segundo, caso em que
// só ele deve ser mostrado na primeira página.
func Dominant(scores []float64) bool {
	return len(scores) >= 2 && scores[1] > 0 && scores[0] >= singleWinnerRatio*scores[1]
}

// stopwords são palavras tão comuns em PT-BR que, num OR, fariam a consulta
// casar com quase toda transcrição e enterrar o resultado certo.
var stopwords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`a o as os um uma uns umas de da do das dos em na no nas nos por pra para com sem
		e ou mas que se ao aos à às pelo pela pelos pelas sobre entre até como qual quais é são foi ser ter tem tá
		eu tu ele ela nós vocês eles elas me te lhe meu minha seu sua isso isto esse essa este esta aquilo mais muito
		já não sim só também quando onde vídeo video vídeos videos`) {
		stopwords[w] = true
	}
}

// BuildFTSQuery transforma texto livre em uma consulta FTS5 segura: cada termo
// vira um prefixo entre aspas ("termo"*), unidos por OR. O ranking por bm25
// ordena quem casa mais termos. Ignora stopwords e tira o "s" final dos termos
// (o prefixo do singular casa o plural: "receitas" acha "receita"). Devolve ""
// se não sobrar termo útil.
func BuildFTSQuery(q string) string {
	terms := strings.FieldsFunc(q, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	seen := map[string]bool{}
	var parts []string
	for _, t := range terms {
		t = strings.ToLower(t)
		if len([]rune(t)) < 2 || stopwords[t] {
			continue
		}
		t = singular(t)
		if seen[t] {
			continue
		}
		seen[t] = true
		parts = append(parts, `"`+t+`"*`)
	}
	return strings.Join(parts, " OR ")
}

// singular tira o "s" (ou "es") final de palavras mais longas que 3 letras. Só
// alarga o prefixo consultado, então não perde resultado.
func singular(t string) string {
	r := []rune(t)
	switch {
	case len(r) > 4 && strings.HasSuffix(t, "es"):
		return string(r[:len(r)-2])
	case len(r) > 3 && strings.HasSuffix(t, "s"):
		return string(r[:len(r)-1])
	}
	return t
}
