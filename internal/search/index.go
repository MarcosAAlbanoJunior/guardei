package search

import (
	"context"
	"math"
	"sort"
	"sync"

	"github.com/MarcosAAlbanoJunior/guardei/internal/store"
)

// Index mantém os embeddings em memória; a varredura é força-bruta.
type Index struct {
	mu   sync.RWMutex
	rows map[int64]entry
}

type entry struct {
	userID int64
	vec    []float32
	norm   float64
}

// Neighbor é um item vizinho da consulta e sua similaridade de cosseno.
type Neighbor struct {
	ID     int64
	Cosine float64
}

// NewIndex cria um índice vazio.
func NewIndex() *Index { return &Index{rows: map[int64]entry{}} }

// Load carrega todos os vetores do banco.
func (ix *Index) Load(ctx context.Context, st *store.Store) error {
	rows, err := st.AllVectors(ctx)
	if err != nil {
		return err
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.rows = make(map[int64]entry, len(rows))
	for _, r := range rows {
		ix.rows[r.ID] = newEntry(r.UserID, r.Vec)
	}
	return nil
}

func newEntry(userID int64, v []float32) entry {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	return entry{userID: userID, vec: v, norm: math.Sqrt(sum)}
}

// Set guarda (ou troca) o vetor de um item.
func (ix *Index) Set(userID, id int64, v []float32) {
	ix.mu.Lock()
	ix.rows[id] = newEntry(userID, v)
	ix.mu.Unlock()
}

// Remove tira um item do índice.
func (ix *Index) Remove(id int64) {
	ix.mu.Lock()
	delete(ix.rows, id)
	ix.mu.Unlock()
}

// Len devolve quantos vetores há no índice.
func (ix *Index) Len() int {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return len(ix.rows)
}

// Nearest devolve até limit itens do usuário com cosseno >= minCosine, do maior
// para o menor. Com allow não nulo, só considera os ids desse conjunto.
func (ix *Index) Nearest(userID int64, q []float32, limit int, minCosine float64, allow map[int64]struct{}) []Neighbor {
	qe := newEntry(userID, q)
	if qe.norm == 0 {
		return nil
	}
	ix.mu.RLock()
	var out []Neighbor
	for id, e := range ix.rows {
		if e.userID != userID || e.norm == 0 || len(e.vec) != len(q) {
			continue
		}
		if _, ok := allow[id]; allow != nil && !ok {
			continue
		}
		var dot float64
		for i, x := range e.vec {
			dot += float64(x) * float64(q[i])
		}
		if c := dot / (e.norm * qe.norm); c >= minCosine {
			out = append(out, Neighbor{id, c})
		}
	}
	ix.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Cosine > out[j].Cosine })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
