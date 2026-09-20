// Package search monta consultas FTS5 e escolhe o que mostrar ao usuário.
package search

import (
	"context"
	"strings"
	"unicode"

	"github.com/marcosjunior/guardei/internal/store"
)

// BuildFTSQuery transforma texto livre em uma consulta FTS5 segura: cada termo
// vira um prefixo entre aspas ("termo"*), unidos por OR. O ranking por bm25
// ordena quem casa mais termos. Devolve "" se não sobrar termo útil.
func BuildFTSQuery(q string) string {
	terms := strings.FieldsFunc(q, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	seen := map[string]bool{}
	var parts []string
	for _, t := range terms {
		t = strings.ToLower(t)
		if len([]rune(t)) < 2 || seen[t] {
			continue
		}
		seen[t] = true
		parts = append(parts, `"`+t+`"*`)
	}
	return strings.Join(parts, " OR ")
}

// Search faz a busca full-text do usuário.
func Search(ctx context.Context, st *store.Store, userID int64, query string, limit int) ([]store.Hit, error) {
	match := BuildFTSQuery(query)
	if match == "" {
		return nil, nil
	}
	return st.SearchFTS(ctx, userID, match, limit)
}

// Pick devolve só o primeiro resultado quando a pontuação dele está bem acima
// do segundo; caso contrário, a lista inteira. O bm25 do SQLite é negativo:
// quanto mais negativo, melhor.
func Pick(hits []store.Hit) []store.Hit {
	if len(hits) < 2 {
		return hits
	}
	best, second := -hits[0].Score, -hits[1].Score
	if best > 0 && best >= 2*second {
		return hits[:1]
	}
	return hits
}
