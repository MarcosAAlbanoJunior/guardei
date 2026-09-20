// Package store guarda itens e estado pendente em SQLite, com índice FTS5.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // driver SQLite em Go puro, sem CGO

	"github.com/MarcosAAlbanoJunior/guardei/migrations"
)

// ErrNotFound indica que o item (ou a espera) não existe para aquele usuário.
var ErrNotFound = errors.New("não encontrado")

// Store guarda itens e o estado pendente dos chats em SQLite.
type Store struct {
	db *sql.DB
}

// Item é um vídeo ou post salvo.
type Item struct {
	ID           int64
	UserID       int64
	URL          string
	CanonicalURL string
	Platform     string
	Title        string
	UserNote     string
	Transcript   string // texto extraído do conteúdo: transcrição do áudio ou texto do post
	Summary      string
	Tags         []string
	Source       string // "transcript" | "page" | "manual"
	CreatedAt    time.Time
}

// Hit é um resultado da busca full-text.
type Hit struct {
	Item  Item
	Score float64 // bm25: quanto menor (mais negativo), melhor
}

// Open abre (ou cria) o banco em path e aplica as migrações.
func Open(ctx context.Context, path string) (*Store, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// Um processo, uso pessoal: uma conexão evita "database is locked".
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrações: %w", err)
	}
	return s, nil
}

// Close fecha o banco.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY)`); err != nil {
		return err
	}
	files, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, name := range files {
		var done int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE name = ?`, name).Scan(&done); err != nil {
			return err
		}
		if done > 0 {
			continue
		}
		body, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(name) VALUES (?)`, name); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

const itemCols = `id, user_id, url, canonical_url, platform, title, user_note, transcript, summary, tags, source, created_at`

// itemColsJoined é itemCols qualificado com "items.", para consultas com JOIN.
var itemColsJoined = "items." + strings.ReplaceAll(itemCols, ", ", ", items.")

type scanner interface{ Scan(...any) error }

func scanItem(r scanner, extra ...any) (Item, error) {
	var (
		it                                     Item
		title, note, transcript, summary, tags sql.NullString
		created                                int64
	)
	dest := append([]any{&it.ID, &it.UserID, &it.URL, &it.CanonicalURL, &it.Platform,
		&title, &note, &transcript, &summary, &tags, &it.Source, &created}, extra...)
	if err := r.Scan(dest...); err != nil {
		return it, err
	}
	it.Title, it.UserNote, it.Transcript, it.Summary = title.String, note.String, transcript.String, summary.String
	if tags.Valid && tags.String != "" {
		_ = json.Unmarshal([]byte(tags.String), &it.Tags)
	}
	it.CreatedAt = time.Unix(created, 0)
	return it, nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func encodeTags(tags []string) any {
	if len(tags) == 0 {
		return nil
	}
	b, _ := json.Marshal(tags)
	return string(b)
}

// Insert grava o item e devolve o id. Item duplicado (mesmo usuário e URL canônica) falha.
func (s *Store) Insert(ctx context.Context, it *Item) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO items (user_id, url, canonical_url, platform, title, user_note, transcript, summary, tags, source, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		it.UserID, it.URL, it.CanonicalURL, it.Platform, nullable(it.Title), nullable(it.UserNote),
		nullable(it.Transcript), nullable(it.Summary), encodeTags(it.Tags), it.Source, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FindByCanonical acha o item do usuário com aquela URL canônica.
func (s *Store) FindByCanonical(ctx context.Context, userID int64, canonical string) (Item, error) {
	it, err := scanItem(s.db.QueryRowContext(ctx,
		`SELECT `+itemCols+` FROM items WHERE user_id = ? AND canonical_url = ?`, userID, canonical))
	if errors.Is(err, sql.ErrNoRows) {
		return it, ErrNotFound
	}
	return it, err
}

// Get devolve um item do usuário pelo id.
func (s *Store) Get(ctx context.Context, userID, id int64) (Item, error) {
	it, err := scanItem(s.db.QueryRowContext(ctx,
		`SELECT `+itemCols+` FROM items WHERE user_id = ? AND id = ?`, userID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return it, ErrNotFound
	}
	return it, err
}

// Delete apaga um item do usuário; o índice full-text acompanha por trigger.
func (s *Store) Delete(ctx context.Context, userID, id int64) error {
	return s.affect(s.db.ExecContext(ctx, `DELETE FROM items WHERE user_id = ? AND id = ?`, userID, id))
}

func (s *Store) affect(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Recent lista os n itens mais recentes do usuário.
func (s *Store) Recent(ctx context.Context, userID int64, n int) ([]Item, error) {
	return s.queryItems(ctx,
		`SELECT `+itemCols+` FROM items WHERE user_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`, userID, n)
}

// queryItems roda uma consulta que devolve as colunas de itemCols.
func (s *Store) queryItems(ctx context.Context, query string, args ...any) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// Count devolve quantos itens o usuário tem.
func (s *Store) Count(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM items WHERE user_id = ?`, userID).Scan(&n)
	return n, err
}

// SearchFTS roda uma consulta MATCH já montada, ordenada por bm25.
func (s *Store) SearchFTS(ctx context.Context, userID int64, match string, limit int) ([]Hit, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+itemColsJoined+`, bm25(items_fts)
		   FROM items_fts JOIN items ON items.id = items_fts.rowid
		  WHERE items_fts MATCH ? AND items.user_id = ?
		  ORDER BY bm25(items_fts) LIMIT ?`, match, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Hit
	for rows.Next() {
		var h Hit
		if h.Item, err = scanItem(rows, &h.Score); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
