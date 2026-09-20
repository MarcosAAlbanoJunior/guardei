package store

import (
	"context"
	"strings"
	"time"
)

// Filter restringe uma busca por período de criação e plataforma. O valor zero
// não restringe nada.
type Filter struct {
	Since    time.Time // inclusivo
	Until    time.Time // exclusivo
	Platform string
}

// IsZero diz se o filtro não restringe nada.
func (f Filter) IsZero() bool {
	return f.Since.IsZero() && f.Until.IsZero() && f.Platform == ""
}

// clause devolve as condições do filtro, prontas para seguir um WHERE, e seus argumentos.
func (f Filter) clause() (string, []any) {
	var sql strings.Builder
	var args []any
	if !f.Since.IsZero() {
		sql.WriteString(" AND items.created_at >= ?")
		args = append(args, f.Since.Unix())
	}
	if !f.Until.IsZero() {
		sql.WriteString(" AND items.created_at < ?")
		args = append(args, f.Until.Unix())
	}
	if f.Platform != "" {
		sql.WriteString(" AND items.platform = ?")
		args = append(args, f.Platform)
	}
	return sql.String(), args
}

// ScoredID é o id de um item e sua pontuação bm25 (menor é melhor).
type ScoredID struct {
	ID   int64
	BM25 float64
}

// SearchFTSIDs roda uma consulta MATCH já montada e devolve só os ids, do mais
// para o menos relevante. Os dados dos itens são lidos depois, por página.
func (s *Store) SearchFTSIDs(ctx context.Context, userID int64, match string, f Filter, limit int) ([]ScoredID, error) {
	where, fargs := f.clause()
	args := append([]any{match, userID}, fargs...)
	rows, err := s.db.QueryContext(ctx,
		`SELECT items.id, bm25(items_fts)
		   FROM items_fts JOIN items ON items.id = items_fts.rowid
		  WHERE items_fts MATCH ? AND items.user_id = ?`+where+`
		  ORDER BY bm25(items_fts) LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ScoredID
	for rows.Next() {
		var r ScoredID
		if err := rows.Scan(&r.ID, &r.BM25); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// IDsByDate lista os ids dos itens do usuário que passam no filtro, do mais novo para o mais antigo.
func (s *Store) IDsByDate(ctx context.Context, userID int64, f Filter, limit int) ([]int64, error) {
	where, fargs := f.clause()
	return s.queryIDs(ctx,
		`SELECT items.id FROM items WHERE items.user_id = ?`+where+` ORDER BY items.created_at DESC, items.id DESC LIMIT ?`,
		append(append([]any{userID}, fargs...), limit)...)
}

// FilteredIDs devolve o conjunto de ids do usuário que passam no filtro, ou nil
// se o filtro não restringe nada (todos passam).
func (s *Store) FilteredIDs(ctx context.Context, userID int64, f Filter) (map[int64]struct{}, error) {
	if f.IsZero() {
		return nil, nil
	}
	where, fargs := f.clause()
	ids, err := s.queryIDs(ctx, `SELECT items.id FROM items WHERE items.user_id = ?`+where, append([]any{userID}, fargs...)...)
	if err != nil {
		return nil, err
	}
	set := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return set, nil
}

// OrderByNewest reordena os ids do mais novo para o mais antigo.
func (s *Store) OrderByNewest(ctx context.Context, userID int64, ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return s.queryIDs(ctx,
		`SELECT id FROM items WHERE user_id = ? AND id IN (`+placeholders(len(ids))+`) ORDER BY created_at DESC, id DESC`,
		append([]any{userID}, idArgs(ids)...)...)
}

// GetMany lê os itens do usuário na ordem dos ids. Ids inexistentes (por exemplo,
// apagados depois da busca) são ignorados.
func (s *Store) GetMany(ctx context.Context, userID int64, ids []int64) ([]Item, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	found, err := s.queryItems(ctx,
		`SELECT `+itemCols+` FROM items WHERE user_id = ? AND id IN (`+placeholders(len(ids))+`)`,
		append([]any{userID}, idArgs(ids)...)...)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]Item, len(found))
	for _, it := range found {
		byID[it.ID] = it
	}
	out := make([]Item, 0, len(found))
	for _, id := range ids {
		if it, ok := byID[id]; ok {
			out = append(out, it)
		}
	}
	return out, nil
}

func (s *Store) queryIDs(ctx context.Context, query string, args ...any) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func placeholders(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }

func idArgs(ids []int64) []any {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return args
}
