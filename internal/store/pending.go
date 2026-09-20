package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Pending é a espera por uma descrição do usuário em um chat.
type Pending struct {
	ChatID int64
	URL    string
	Reason string // por que caiu no modo manual (vazio ao atualizar um item)
	ItemID int64  // 0 = descrever um link novo; senão, atualizar este item
}

// SetPending guarda (ou substitui) a espera do chat.
func (s *Store) SetPending(ctx context.Context, p Pending) error {
	var itemID any
	if p.ItemID != 0 {
		itemID = p.ItemID
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO pending (chat_id, url, reason, item_id, created_at) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(chat_id) DO UPDATE SET url = excluded.url, reason = excluded.reason,
		   item_id = excluded.item_id, created_at = excluded.created_at`,
		p.ChatID, p.URL, p.Reason, itemID, time.Now().Unix())
	return err
}

func (s *Store) GetPending(ctx context.Context, chatID int64) (Pending, error) {
	p := Pending{ChatID: chatID}
	var itemID sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT url, reason, item_id FROM pending WHERE chat_id = ?`, chatID).
		Scan(&p.URL, &p.Reason, &itemID)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	p.ItemID = itemID.Int64
	return p, err
}

// ClearPending devolve true se havia espera para encerrar.
func (s *Store) ClearPending(ctx context.Context, chatID int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM pending WHERE chat_id = ?`, chatID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
