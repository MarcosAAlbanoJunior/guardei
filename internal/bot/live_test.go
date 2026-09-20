//go:build live

// Testes ponta a ponta com Gemini, yt-dlp e sites reais. Não rodam por padrão:
//
//	set -a; . ./.env; set +a
//	LIVE_VIDEO_URLS="https://youtu.be/..." go test -tags live ./internal/bot -run LiveVideos -v
//	LIVE_POST_URLS="https://x.com/..."    go test -tags live ./internal/bot -run LivePosts -v
//
// LIVE_QUERIES ("termo;outro termo") faz buscas depois de salvar e
// LIVE_MAX_SECONDS muda o limite de duração dos vídeos (padrão 600).
package bot

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/marcosjunior/guardei/internal/ai"
	"github.com/marcosjunior/guardei/internal/extract"
	"github.com/marcosjunior/guardei/internal/page"
	"github.com/marcosjunior/guardei/internal/search"
	"github.com/marcosjunior/guardei/internal/store"
)

// liveSession é um Handler real com banco temporário; last guarda a última resposta.
type liveSession struct {
	t    *testing.T
	h    *Handler
	last string
}

func newLiveSession(t *testing.T) *liveSession {
	t.Helper()
	key := os.Getenv("GEMINI_API_KEY")
	if key == "" {
		t.Skip("defina GEMINI_API_KEY")
	}
	st, err := store.Open(context.Background(), t.TempDir()+"/live.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	maxSeconds := 600
	if v, err := strconv.Atoi(os.Getenv("LIVE_MAX_SECONDS")); err == nil {
		maxSeconds = v
	}
	var extractor extract.Extractor = extract.Nop{}
	if extract.Available("yt-dlp") == nil {
		extractor = extract.NewYtDlp("yt-dlp", time.Duration(maxSeconds)*time.Second)
	}
	client := ai.New(key, "gemini-2.5-flash-lite", "gemini-embedding-2")

	s := &liveSession{t: t}
	s.h = &Handler{
		Store: st, AI: client, Extractor: extractor, Pages: page.NewHTTP(),
		Searcher: &search.Searcher{Store: st, AI: client, Index: search.NewIndex()},
		Allowed:  map[int64]bool{1: true}, SearchLimit: 5,
		Reply: func(_ context.Context, _ int64, text string) {
			s.last = text
			t.Logf("  >> %s", strings.SplitN(text, "\n", 2)[0])
		},
	}
	return s
}

func (s *liveSession) send(text string) string {
	s.h.Handle(context.Background(), 1, 1, text)
	return s.last
}

// each envia cada link da variável, cancelando a espera entre eles.
func (s *liveSession) each(env string, check func(url, reply string)) {
	for _, u := range strings.Split(os.Getenv(env), ",") {
		if u = strings.TrimSpace(u); u == "" {
			continue
		}
		start := time.Now()
		reply := s.send(u)
		s.t.Logf("--- %s (%s)\n%s", u, time.Since(start).Round(time.Millisecond), reply)
		if check != nil {
			check(u, reply)
		}
		s.send("/cancelar")
	}
}

func (s *liveSession) queries() {
	for _, q := range strings.Split(os.Getenv("LIVE_QUERIES"), ";") {
		if q != "" {
			s.t.Logf("=== busca %q\n%s", q, s.send(q))
		}
	}
}

func TestLiveVideos(t *testing.T) {
	if os.Getenv("LIVE_VIDEO_URLS") == "" {
		t.Skip("defina LIVE_VIDEO_URLS")
	}
	s := newLiveSession(t)
	s.each("LIVE_VIDEO_URLS", func(u, reply string) {
		if !strings.Contains(reply, "transcrito") {
			t.Errorf("não transcreveu %s: %s", u, reply)
		}
	})
	s.queries()
}

func TestLivePosts(t *testing.T) {
	if os.Getenv("LIVE_POST_URLS") == "" {
		t.Skip("defina LIVE_POST_URLS")
	}
	s := newLiveSession(t)
	s.each("LIVE_POST_URLS", nil) // posts privados caem no pedido de descrição: só registra
	s.queries()
}
