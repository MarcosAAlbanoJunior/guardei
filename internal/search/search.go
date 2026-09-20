// Package search combina FTS5 e busca vetorial e escolhe o que mostrar ao usuário.
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
	// MinCosine descarta vizinhos sem relação com a consulta. Medido com o
	// gemini-embedding-2: relevantes ficam em 0,67–0,79 e irrelevantes até 0,62.
	MinCosine = 0.65

	rrfK         = 60 // constante clássica do reciprocal rank fusion
	candidates   = 20
	embedTimeout = 15 * time.Second

	// singleWinnerRatio: o primeiro resultado sozinho, se valer 1,5x o segundo.
	singleWinnerRatio = 1.5
)

// Result é um resultado de busca; Score maior é melhor.
type Result struct {
	Item  store.Item
	Score float64
}

// Searcher busca nos itens de um usuário: full-text sempre, e híbrida (RRF com
// a busca vetorial) quando há IA e vetores.
type Searcher struct {
	Store *store.Store
	AI    ai.Client
	Index *Index
}

// stopwords são palavras tão comuns em PT-BR que, num OR, fariam a consulta
// casar com quase toda transcrição e enterrar o resultado certo.
var stopwords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`a o as os um uma uns umas de da do das dos em na no nas nos por pra para com sem
		e ou mas que se ao aos à às pelo pela pelos pelas sobre entre até como qual quais é são foi ser ter tem tá
		eu tu ele ela nós vocês eles elas me te lhe meu minha seu sua isso isto esse essa este esta aquilo mais muito
		já não sim só também quando onde vídeo video`) {
		stopwords[w] = true
	}
}

// Remember guarda o vetor de um item no índice em memória.
func (s *Searcher) Remember(userID, id int64, v []float32) { s.Index.Set(userID, id, v) }

// Forget tira um item do índice em memória (apagado ou com vetor desatualizado).
func (s *Searcher) Forget(id int64) { s.Index.Remove(id) }

// BuildFTSQuery transforma texto livre em uma consulta FTS5 segura: cada termo
// vira um prefixo entre aspas ("termo"*), unidos por OR. O ranking por bm25
// ordena quem casa mais termos. Ignora stopwords. Devolve "" se não sobrar termo útil.
func BuildFTSQuery(q string) string {
	terms := strings.FieldsFunc(q, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	seen := map[string]bool{}
	var parts []string
	for _, t := range terms {
		t = strings.ToLower(t)
		if len([]rune(t)) < 2 || stopwords[t] || seen[t] {
			continue
		}
		seen[t] = true
		parts = append(parts, `"`+t+`"*`)
	}
	return strings.Join(parts, " OR ")
}

// Search faz a busca do usuário: FTS5 sempre; híbrida (RRF) quando há IA e
// vetores. Falha no embedding da consulta cai no FTS5, sem erro para o usuário.
func (s *Searcher) Search(ctx context.Context, userID int64, query string, limit int) ([]Result, error) {
	n := max(limit, candidates)
	var fts []store.Hit
	if match := BuildFTSQuery(query); match != "" {
		var err error
		if fts, err = s.Store.SearchFTS(ctx, userID, match, n); err != nil {
			return nil, err
		}
	}

	var vec []Neighbor
	if s.AI != nil && s.AI.Enabled() && s.Index != nil && s.Index.Len() > 0 {
		ectx, cancel := context.WithTimeout(ctx, embedTimeout)
		q, err := s.AI.Embed(ectx, query, ai.TaskQuery)
		cancel()
		if err != nil {
			slog.Warn("embedding da consulta falhou; usando só FTS", "err", err)
		} else {
			vec = s.Index.Nearest(userID, q, n, MinCosine)
		}
	}

	if len(vec) == 0 {
		out := make([]Result, len(fts))
		for i, h := range fts {
			out[i] = Result{h.Item, -h.Score} // bm25 é negativo: inverte para "maior é melhor"
		}
		return cut(out, limit), nil
	}
	return s.fuse(ctx, userID, fts, vec, limit)
}

// fuse une os dois rankings por reciprocal rank fusion.
func (s *Searcher) fuse(ctx context.Context, userID int64, fts []store.Hit, vec []Neighbor, limit int) ([]Result, error) {
	score := map[int64]float64{}
	items := map[int64]store.Item{}
	for i, h := range fts {
		score[h.Item.ID] += 1.0 / float64(rrfK+i+1)
		items[h.Item.ID] = h.Item
	}
	for i, v := range vec {
		score[v.ID] += 1.0 / float64(rrfK+i+1)
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
	var out []Result
	for _, id := range ids {
		if len(out) == limit {
			break
		}
		it, ok := items[id]
		if !ok {
			var err error
			if it, err = s.Store.Get(ctx, userID, id); err != nil {
				continue // apagado entre a varredura e agora
			}
		}
		out = append(out, Result{it, score[id]})
	}
	return out, nil
}

func cut(r []Result, limit int) []Result {
	if len(r) > limit {
		return r[:limit]
	}
	return r
}

// Pick devolve só o primeiro resultado quando a pontuação dele está bem acima
// do segundo; caso contrário, a lista inteira.
func Pick(r []Result) []Result {
	if len(r) < 2 || r[1].Score <= 0 {
		return r
	}
	if r[0].Score >= singleWinnerRatio*r[1].Score {
		return r[:1]
	}
	return r
}
