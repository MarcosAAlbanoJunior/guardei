package store

import (
	"context"
	"encoding/binary"
	"math"
)

// EncodeVector serializa float32 em little-endian, o formato da coluna embedding.
func EncodeVector(v []float32) []byte {
	if len(v) == 0 {
		return nil
	}
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(f))
	}
	return b
}

func DecodeVector(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v
}

type VectorRow struct {
	ID, UserID int64
	Vec        []float32
}

// SetEmbedding grava o vetor do item; nil apaga (o item volta à fila do /reindexar).
func (s *Store) SetEmbedding(ctx context.Context, userID, id int64, v []float32) error {
	var blob any
	if len(v) > 0 {
		blob = EncodeVector(v)
	}
	return s.affect(s.db.ExecContext(ctx,
		`UPDATE items SET embedding = ? WHERE user_id = ? AND id = ?`, blob, userID, id))
}

// UpdateContent troca descrição, resumo e tags de uma vez e limpa o embedding,
// que deixou de valer. Reindexa o FTS via trigger.
func (s *Store) UpdateContent(ctx context.Context, userID, id int64, note, summary string, tags []string) error {
	return s.affect(s.db.ExecContext(ctx,
		`UPDATE items SET user_note = ?, summary = ?, tags = ?, embedding = NULL WHERE user_id = ? AND id = ?`,
		nullable(note), nullable(summary), encodeTags(tags), userID, id))
}

// AllVectors lê todos os embeddings, para carregar o índice em memória.
func (s *Store) AllVectors(ctx context.Context) ([]VectorRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, user_id, embedding FROM items WHERE embedding IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []VectorRow
	for rows.Next() {
		var r VectorRow
		var blob []byte
		if err := rows.Scan(&r.ID, &r.UserID, &blob); err != nil {
			return nil, err
		}
		r.Vec = DecodeVector(blob)
		out = append(out, r)
	}
	return out, rows.Err()
}

// WithoutEmbedding lista os itens do usuário ainda sem vetor.
func (s *Store) WithoutEmbedding(ctx context.Context, userID int64) ([]Item, error) {
	return s.queryItems(ctx,
		`SELECT `+itemCols+` FROM items WHERE user_id = ? AND embedding IS NULL ORDER BY id`, userID)
}

func (s *Store) CountEmbedded(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM items WHERE user_id = ? AND embedding IS NOT NULL`, userID).Scan(&n)
	return n, err
}
