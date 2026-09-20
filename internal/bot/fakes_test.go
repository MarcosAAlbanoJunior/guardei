package bot

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/MarcosAAlbanoJunior/guardei/internal/ai"
	"github.com/MarcosAAlbanoJunior/guardei/internal/extract"
	"github.com/MarcosAAlbanoJunior/guardei/internal/page"
	"github.com/MarcosAAlbanoJunior/guardei/internal/search"
	"github.com/MarcosAAlbanoJunior/guardei/internal/store"
)

type harness struct {
	t    *testing.T
	h    *Handler
	mu   sync.Mutex
	last string
	all  []string // todas as respostas, em ordem
}

func newHarness(t *testing.T) *harness { return newHarnessAI(t, ai.New("", "", "")) }

func newHarnessAI(t *testing.T, client ai.Client) *harness {
	t.Helper()
	st, err := store.Open(context.Background(), t.TempDir()+"/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hn := &harness{t: t}
	hn.h = &Handler{Store: st, AI: client, AIInfo: "fake", Extractor: extract.Nop{},
		Searcher: &search.Searcher{Store: st, AI: client, Index: search.NewIndex()},
		Allowed:  map[int64]bool{1: true}, SearchLimit: 5,
		Reply: func(_ context.Context, _ int64, text string) {
			hn.mu.Lock()
			defer hn.mu.Unlock()
			hn.last = text
			hn.all = append(hn.all, text)
		}}
	return hn
}

func (hn *harness) say(text string) string {
	hn.last = ""
	hn.h.Handle(context.Background(), 1, 1, text)
	return hn.last
}

func (hn *harness) expect(text, contains string) {
	hn.t.Helper()
	if got := hn.say(text); !strings.Contains(got, contains) {
		hn.t.Fatalf("%q -> %q; esperava conter %q", text, got, contains)
	}
}

// fakeAI: resumo = "Resumo: "+texto; embeddings em 2 dimensões (comida, esporte).
type fakeAI struct {
	down  bool
	audio ai.Analysis // resposta de AnalyzeAudio
}

func (f fakeAI) AnalyzePost(_ context.Context, p ai.Post) (ai.Analysis, error) {
	if f.down {
		return ai.Analysis{}, errors.New("fora do ar")
	}
	if strings.Contains(p.Text, "ENTRE OU CADASTRE-SE") {
		return ai.Analysis{}, nil // has_content=false
	}
	return ai.Analysis{HasContent: true, Title: "Título IA", Summary: "Resumo do post: " + p.Text, Tags: []string{"post", "tag2"}}, nil
}

func (f fakeAI) AnalyzeTranscript(_ context.Context, t string) (ai.Analysis, error) {
	if f.down {
		return ai.Analysis{}, errors.New("fora do ar")
	}
	return ai.Analysis{Summary: "Resumo geral da transcrição", Tags: []string{"longo"}}, nil
}

func (fakeAI) Enabled() bool { return true }

func (f fakeAI) AnalyzeAudio(context.Context, []byte, string) (ai.Analysis, error) {
	if f.down {
		return ai.Analysis{}, errors.New("fora do ar")
	}
	return f.audio, nil
}

func (f fakeAI) AnalyzeText(_ context.Context, text string) (ai.Analysis, error) {
	if f.down {
		return ai.Analysis{}, errors.New("fora do ar")
	}
	return ai.Analysis{HasSpeech: true, Summary: "Resumo: " + text, Tags: []string{"tag1", "tag2"}}, nil
}

func (f fakeAI) Embed(_ context.Context, text string, _ ai.EmbedTask) ([]float32, error) {
	if f.down {
		return nil, errors.New("fora do ar")
	}
	t := strings.ToLower(text)
	switch {
	case strings.Contains(t, "queijo"), strings.Contains(t, "comida"):
		return []float32{1, 0}, nil
	case strings.Contains(t, "perna"), strings.Contains(t, "esporte"):
		return []float32{0, 1}, nil
	}
	return []float32{0, 0}, nil // sem relação com nenhum conceito
}

// fakeExtractor: YouTube e X; devolve o áudio ou o erro configurado.
type fakeExtractor struct {
	err          error
	calls        int
	segments     int  // 0 = um só
	startedFirst bool // erro só depois de começar o download (o vídeo existe, mas falhou)
}

func (*fakeExtractor) Supports(u *url.URL) bool {
	return strings.Contains(u.Host, "youtu") || u.Host == "x.com"
}

func (f *fakeExtractor) Audio(_ context.Context, _ *url.URL, started func()) (extract.AudioFile, error) {
	f.calls++
	if f.err != nil && !f.startedFirst {
		return extract.AudioFile{}, f.err // falhou ao sondar (ex.: tweet sem vídeo)
	}
	if started != nil {
		started()
	}
	if f.err != nil {
		return extract.AudioFile{}, f.err
	}
	n := max(f.segments, 1)
	return extract.AudioFile{Segments: make([][]byte, n), Mime: "audio/mp3", Title: "Como fazer pão de queijo"}, nil
}

func speech() ai.Analysis {
	return ai.Analysis{HasSpeech: true, Transcript: "hoje vamos fazer pão de queijo",
		Summary: "Passo a passo de pão de queijo.", Tags: []string{"receita", "pão de queijo"}}
}

// fakePages devolve a página (ou o erro) configurada e conta as leituras.
type fakePages struct {
	pg    page.Page
	err   error
	calls int
	urls  []string
}

func (f *fakePages) Read(_ context.Context, u string) (page.Page, error) {
	f.calls++
	f.urls = append(f.urls, u)
	return f.pg, f.err
}

func withPages(t *testing.T, client ai.Client, pg *fakePages) *harness {
	hn := newHarnessAI(t, client)
	hn.h.Pages = pg
	return hn
}

func (hn *harness) said(sub string) bool {
	for _, m := range hn.all {
		if strings.Contains(m, sub) {
			return true
		}
	}
	return false
}
