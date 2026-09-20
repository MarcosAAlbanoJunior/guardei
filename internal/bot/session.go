package bot

import (
	"sync"
	"time"

	"github.com/MarcosAAlbanoJunior/guardei/internal/search"
)

// sessionTTL é quanto tempo os botões de uma busca continuam valendo.
const sessionTTL = 30 * time.Minute

// searchSession guarda a lista ordenada de uma busca (só ids) para "Ver mais" e
// os botões de período não repetirem a busca nem a chamada à IA. Os dados dos
// itens são lidos do banco página por página.
type searchSession struct {
	id       int64
	userID   int64
	query    search.Query
	newest   bool
	pageSize int
	ranking  search.Ranking
	shown    int          // quantos ids da lista já foram mostrados
	handled  map[int]bool // mensagens cujo botão já foi tratado (toque duplo)
	created  time.Time
}

// sessionStore guarda a última busca de cada chat, em memória: é estado
// descartável, e uma reinicialização só faz os botões antigos expirarem.
type sessionStore struct {
	mu     sync.Mutex
	m      map[int64]*searchSession
	nextID int64
}

// put grava a sessão como a busca atual do chat e devolve seu id.
func (s *sessionStore) put(chatID int64, sess *searchSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[int64]*searchSession{}
	}
	now := time.Now()
	for id, old := range s.m { // mantém o mapa pequeno
		if now.Sub(old.created) > sessionTTL {
			delete(s.m, id)
		}
	}
	s.nextID++
	sess.id, sess.created, sess.handled = s.nextID, now, map[int]bool{}
	s.m[chatID] = sess
}

// get devolve a sessão do chat se ela ainda vale e for a de id sid.
func (s *sessionStore) get(chatID, sid int64) *searchSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.m[chatID]
	if sess == nil || sess.id != sid || time.Since(sess.created) > sessionTTL {
		return nil
	}
	return sess
}
