package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/marcosjunior/guardei/migrations"
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
