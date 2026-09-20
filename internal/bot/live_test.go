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

// Ponta a ponta com yt-dlp e Gemini reais. Só roda com GEMINI_API_KEY e
// LIVE_VIDEO_URLS (links separados por vírgula). Serve para avaliar a
// qualidade do modelo em PT-BR com vídeos de verdade.
func TestLiveTranscription(t *testing.T) {
	key, urls := os.Getenv("GEMINI_API_KEY"), os.Getenv("LIVE_VIDEO_URLS")
	if key == "" || urls == "" {
		t.Skip("defina GEMINI_API_KEY e LIVE_VIDEO_URLS")
	}
	if err := extract.Available("yt-dlp"); err != nil {
		t.Skip(err)
	}
	st, err := store.Open(context.Background(), t.TempDir()+"/t.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	maxSec := 600
	if v, err := strconv.Atoi(os.Getenv("LIVE_MAX_SECONDS")); err == nil {
		maxSec = v
	}
	client := ai.New(key, "gemini-2.5-flash-lite", "gemini-embedding-2")
	var last string
	h := &Handler{Store: st, AI: client, Extractor: extract.NewYtDlp("yt-dlp", time.Duration(maxSec)*time.Second),
		Searcher: &search.Searcher{Store: st, AI: client, Index: search.NewIndex()},
		Allowed:  map[int64]bool{1: true}, SearchLimit: 5,
		Reply: func(_ context.Context, _ int64, text string) { last = text; t.Log(text) }}

	for _, u := range strings.Split(urls, ",") {
		start := time.Now()
		h.Handle(context.Background(), 1, 1, strings.TrimSpace(u))
		t.Logf("--- %s levou %s", u, time.Since(start).Round(time.Second))
		if !strings.Contains(last, "transcrito") {
			t.Errorf("não transcreveu %s: %s", u, last)
		}
	}
	for _, q := range strings.Split(os.Getenv("LIVE_QUERIES"), ";") {
		if q != "" {
			h.Handle(context.Background(), 1, 1, q)
			t.Logf("=== busca %q", q)
		}
	}
	items, _ := st.Recent(context.Background(), 1, 20)
	for _, it := range items {
		t.Logf("#%d transcript(%d chars): %.200q", it.ID, len(it.Transcript), it.Transcript)
	}
}

// Posts reais (texto) com leitura de página e Gemini reais: LIVE_POST_URLS.
func TestLivePosts(t *testing.T) {
	key, urls := os.Getenv("GEMINI_API_KEY"), os.Getenv("LIVE_POST_URLS")
	if key == "" || urls == "" {
		t.Skip("defina GEMINI_API_KEY e LIVE_POST_URLS")
	}
	st, err := store.Open(context.Background(), t.TempDir()+"/t.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	client := ai.New(key, "gemini-2.5-flash-lite", "gemini-embedding-2")
	var last string
	h := &Handler{Store: st, AI: client, Extractor: extract.Nop{}, Pages: page.NewHTTP(),
		Searcher: &search.Searcher{Store: st, AI: client, Index: search.NewIndex()},
		Allowed:  map[int64]bool{1: true}, SearchLimit: 5,
		Reply: func(_ context.Context, _ int64, text string) { last = text }}

	for _, u := range strings.Split(urls, ",") {
		u = strings.TrimSpace(u)
		start := time.Now()
		h.Handle(context.Background(), 1, 1, u)
		t.Logf("--- %s (%s)\n%s", u, time.Since(start).Round(time.Millisecond), last)
		h.Handle(context.Background(), 1, 1, "/cancelar") // não deixa espera pendente entre os links
	}
	for _, q := range strings.Split(os.Getenv("LIVE_QUERIES"), ";") {
		if q != "" {
			h.Handle(context.Background(), 1, 1, q)
			t.Logf("=== busca %q\n%s", q, last)
		}
	}
}
