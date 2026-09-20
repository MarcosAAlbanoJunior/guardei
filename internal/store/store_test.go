package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/MarcosAAlbanoJunior/guardei/migrations"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), t.TempDir()+"/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestInsertSearchUpdateDelete(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	id, err := s.Insert(ctx, &Item{UserID: 1, URL: "https://a", CanonicalURL: "https://a", Platform: "other",
		UserNote: "Receita de pão de queijo mineiro", Source: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Insert(ctx, &Item{UserID: 1, URL: "https://a", CanonicalURL: "https://a", Platform: "other", Source: "manual"}); err == nil {
		t.Fatal("esperava erro de duplicata")
	}
	if _, err := s.Insert(ctx, &Item{UserID: 2, URL: "https://b", CanonicalURL: "https://b", Platform: "other",
		UserNote: "pão de queijo do outro usuário", Source: "manual"}); err != nil {
		t.Fatal(err)
	}

	// diacríticos e prefixo
	hits, err := s.SearchFTS(ctx, 1, `"pao"* OR "quei"*`, 5)
	if err != nil || len(hits) != 1 || hits[0].Item.ID != id {
		t.Fatalf("busca: %v %v", hits, err)
	}

	if err := s.UpdateContent(ctx, 1, id, "treino de perna na academia", "", nil); err != nil {
		t.Fatal(err)
	}
	if hits, _ = s.SearchFTS(ctx, 1, `"queijo"*`, 5); len(hits) != 0 {
		t.Fatalf("índice não atualizou: %v", hits)
	}
	if hits, _ = s.SearchFTS(ctx, 1, `"perna"*`, 5); len(hits) != 1 {
		t.Fatalf("índice não atualizou: %v", hits)
	}

	if err := s.Delete(ctx, 2, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("apagou item de outro usuário: %v", err)
	}
	if err := s.Delete(ctx, 1, id); err != nil {
		t.Fatal(err)
	}
	if hits, _ = s.SearchFTS(ctx, 1, `"perna"*`, 5); len(hits) != 0 {
		t.Fatalf("índice não removeu: %v", hits)
	}
}

func TestPending(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	if _, err := s.GetPending(ctx, 9); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	s.SetPending(ctx, Pending{ChatID: 9, URL: "u1", Reason: "r1"})
	s.SetPending(ctx, Pending{ChatID: 9, URL: "u2", Reason: "r2"})
	p, err := s.GetPending(ctx, 9)
	if err != nil || p.URL != "u2" {
		t.Fatalf("%+v %v", p, err)
	}
	if ok, _ := s.ClearPending(ctx, 9); !ok {
		t.Fatal("deveria ter limpado")
	}
}

func TestPendingItemID(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	s.SetPending(ctx, Pending{ChatID: 1, URL: "u", ItemID: 42})
	p, err := s.GetPending(ctx, 1)
	if err != nil || p.ItemID != 42 || p.Reason != "" {
		t.Fatalf("%+v %v", p, err)
	}
	// substituir por uma espera de link novo zera o item
	s.SetPending(ctx, Pending{ChatID: 1, URL: "u2", Reason: "sem chave"})
	if p, _ = s.GetPending(ctx, 1); p.ItemID != 0 || p.Reason != "sem chave" {
		t.Fatalf("%+v", p)
	}
}

// Um banco criado antes da coluna item_id guardava "atualizar:<id>" em reason.
func TestMigrationConvertsLegacyPending(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/legacy.db"
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	first, _ := migrations.FS.ReadFile("0001_init.sql")
	for _, q := range []string{
		string(first),
		`CREATE TABLE schema_migrations (name TEXT PRIMARY KEY)`,
		`INSERT INTO schema_migrations VALUES ('0001_init.sql')`,
		`INSERT INTO pending VALUES (1, 'u1', 'atualizar:7', 0)`,
		`INSERT INTO pending VALUES (2, 'u2', 'esta plataforma não tem extração', 0)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if p, _ := s.GetPending(ctx, 1); p.ItemID != 7 || p.Reason != "" {
		t.Fatalf("legado não convertido: %+v", p)
	}
	if p, _ := s.GetPending(ctx, 2); p.ItemID != 0 || p.Reason == "" {
		t.Fatalf("espera de link novo alterada: %+v", p)
	}
}

func TestGetFindRecentCountAreScopedToUser(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	a, _ := s.Insert(ctx, &Item{UserID: 1, URL: "u1", CanonicalURL: "c1", Platform: "x", Title: "T", Tags: []string{"a", "b"}, Source: "page"})
	b, _ := s.Insert(ctx, &Item{UserID: 1, URL: "u2", CanonicalURL: "c2", Platform: "x", Source: "manual"})
	s.Insert(ctx, &Item{UserID: 2, URL: "u1", CanonicalURL: "c1", Platform: "x", Source: "manual"}) // mesma URL, outro usuário

	it, err := s.Get(ctx, 1, a)
	if err != nil || it.Title != "T" || len(it.Tags) != 2 || it.Source != "page" || it.CreatedAt.IsZero() {
		t.Fatalf("%+v %v", it, err)
	}
	if _, err := s.Get(ctx, 2, a); !errors.Is(err, ErrNotFound) {
		t.Fatalf("usuário 2 leu item do 1: %v", err)
	}
	if f, err := s.FindByCanonical(ctx, 1, "c2"); err != nil || f.ID != b {
		t.Fatalf("%+v %v", f, err)
	}
	if _, err := s.FindByCanonical(ctx, 1, "nao-existe"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if n, _ := s.Count(ctx, 1); n != 2 {
		t.Fatalf("count=%d", n)
	}
	recent, _ := s.Recent(ctx, 1, 10)
	if len(recent) != 2 || recent[0].ID != b { // mais novo primeiro
		t.Fatalf("%+v", recent)
	}
	if recent, _ = s.Recent(ctx, 1, 1); len(recent) != 1 {
		t.Fatalf("limite ignorado: %d", len(recent))
	}
}

func TestEmbeddingLifecycle(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	id, _ := s.Insert(ctx, &Item{UserID: 1, URL: "u", CanonicalURL: "c", Platform: "x", UserNote: "nota", Source: "manual"})

	if missing, _ := s.WithoutEmbedding(ctx, 1); len(missing) != 1 {
		t.Fatal("item novo deveria estar sem embedding")
	}
	if err := s.SetEmbedding(ctx, 1, id, []float32{0.5, -1.25, 3}); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.CountEmbedded(ctx, 1); n != 1 {
		t.Fatalf("embedded=%d", n)
	}
	rows, err := s.AllVectors(ctx)
	if err != nil || len(rows) != 1 || rows[0].ID != id || rows[0].UserID != 1 ||
		len(rows[0].Vec) != 3 || rows[0].Vec[1] != -1.25 {
		t.Fatalf("%+v %v", rows, err)
	}
	if err := s.SetEmbedding(ctx, 2, id, []float32{1}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outro usuário gravou vetor: %v", err)
	}

	// trocar o conteúdo invalida o vetor
	if err := s.UpdateContent(ctx, 1, id, "nova nota", "resumo", []string{"t"}); err != nil {
		t.Fatal(err)
	}
	if missing, _ := s.WithoutEmbedding(ctx, 1); len(missing) != 1 || missing[0].Summary != "resumo" {
		t.Fatalf("vetor antigo deveria ter sido limpo: %+v", missing)
	}
}

func TestVectorEncoding(t *testing.T) {
	in := []float32{0, 1.5, -2.25, 1e-7}
	out := DecodeVector(EncodeVector(in))
	if len(out) != len(in) {
		t.Fatal(out)
	}
	for i := range in {
		if in[i] != out[i] {
			t.Errorf("%d: %v != %v", i, in[i], out[i])
		}
	}
	if EncodeVector(nil) != nil {
		t.Error("vetor vazio deveria virar nil (NULL no banco)")
	}
}

func TestOpenTwiceKeepsDataAndMigrationsOnce(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/again.db"
	s1, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	s1.Insert(ctx, &Item{UserID: 1, URL: "u", CanonicalURL: "c", Platform: "x", Source: "manual"})
	s1.Close()

	s2, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("reabrir falhou (migração reaplicada?): %v", err)
	}
	defer s2.Close()
	if n, _ := s2.Count(ctx, 1); n != 1 {
		t.Fatalf("perdeu dados: %d", n)
	}
}
