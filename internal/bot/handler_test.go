package bot

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/marcosjunior/guardei/internal/ai"
	"github.com/marcosjunior/guardei/internal/extract"
	"github.com/marcosjunior/guardei/internal/search"
	"github.com/marcosjunior/guardei/internal/store"
)

type harness struct {
	t    *testing.T
	h    *Handler
	last string
}

func newHarness(t *testing.T) *harness { return newHarnessAI(t, ai.New("", "", "")) }

func newHarnessAI(t *testing.T, client ai.AIClient) *harness {
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
		Reply: func(_ context.Context, _ int64, text string) { hn.last = text }}
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

func TestFlowLinkWithoutNoteAsksThenSaves(t *testing.T) {
	hn := newHarness(t)
	hn.expect("https://www.instagram.com/reel/AbC/?igshid=1", "Me diga do que ele trata")
	hn.expect("receita de pão de queijo mineiro", "Salvo (#1, instagram)")
	hn.expect("queijo", "https://www.instagram.com/reel/AbC/")
	hn.expect("futebol", "Não achei nada")
}

func TestLinkWithNoteSavesDirectly(t *testing.T) {
	hn := newHarness(t)
	hn.expect("https://youtu.be/abc treino de perna", "Salvo (#1, youtube)")
	// mensagem sem link agora é busca, não descrição
	hn.expect("perna", "#1")
}

func TestDuplicateOffersUpdate(t *testing.T) {
	hn := newHarness(t)
	hn.expect("https://youtu.be/abc treino de perna", "Salvo")
	hn.expect("https://www.youtube.com/watch?v=abc&utm_source=x", "já está salvo (#1)")
	hn.expect("treino de costas", "atualizada")
	hn.expect("costas", "#1")
	hn.expect("perna", "Não achei nada")
}

func TestCancelAndNewLinkEndPending(t *testing.T) {
	hn := newHarness(t)
	hn.expect("https://example.com/a", "Me diga")
	hn.expect("/cancelar", "Cancelado")
	hn.expect("algo qualquer", "Não achei nada") // virou busca

	hn.expect("https://example.com/a", "Me diga")
	hn.expect("https://example.com/b", "Me diga") // substitui a espera
	hn.expect("descrição do b", "Salvo (#1, other)")
}

func TestEditDeleteRecentes(t *testing.T) {
	hn := newHarness(t)
	hn.expect("https://example.com/a bolo de cenoura", "Salvo")
	hn.expect("/editar 1 bolo de fubá", "atualizada")
	hn.expect("/editar 1", "Envie a nova descrição")
	hn.expect("bolo de milho", "atualizada")
	hn.expect("/recentes", "bolo de milho")
	hn.expect("/apagar 1", "apagado")
	hn.expect("/apagar 1", "Não achei")
	hn.expect("/recentes", "Ainda não há")
}

func TestUnauthorizedIgnored(t *testing.T) {
	hn := newHarness(t)
	hn.last = ""
	hn.h.Handle(context.Background(), 99, 99, "/status")
	if hn.last != "" {
		t.Fatalf("respondeu a usuário não autorizado: %q", hn.last)
	}
}

// fakeAI: resumo = "Resumo: "+texto; embeddings em 2 dimensões (comida, esporte).
type fakeAI struct {
	down  bool
	audio ai.Analysis // resposta de AnalyzeAudio
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

func TestAISavesSummaryTagsAndFindsBySemantics(t *testing.T) {
	hn := newHarnessAI(t, fakeAI{})
	hn.expect("https://example.com/a pão de queijo mineiro", "Resumo: pão de queijo mineiro")
	hn.expect("/recentes", "Resumo: pão de queijo mineiro")
	hn.expect("comida", "#1") // "comida" não está no texto: só o embedding acha
	hn.expect("/status", "Com embedding: 1")

	hn.expect("/editar 1 treino de perna", "atualizada")
	hn.expect("esporte", "#1")
	hn.expect("comida", "Não achei nada")
}

func TestAIDownDegradesToManualAndReindexRecovers(t *testing.T) {
	down := fakeAI{down: true}
	hn := newHarnessAI(t, down)
	// IA fora do ar: salva só com a descrição, sem erro para o usuário.
	hn.expect("https://example.com/a pão de queijo", "Salvo (#1, other):\npão de queijo")
	hn.expect("queijo", "#1") // FTS continua funcionando
	hn.expect("comida", "Não achei nada")

	// IA volta: /reindexar completa resumo e embedding.
	hn.h.AI = fakeAI{}
	hn.h.Searcher.AI = fakeAI{}
	hn.expect("/reindexar", "Reindexados 1 de 1")
	hn.expect("comida", "#1")
	hn.expect("/reindexar", "já têm embedding")
	hn.expect("/apagar 1", "apagado")
	hn.expect("comida", "Não achei nada")
}

func TestReindexWithoutAI(t *testing.T) {
	hn := newHarness(t)
	hn.expect("/reindexar", "desligada")
	hn.expect("/status", "desligada")
}

// fakeExtractor: só YouTube; devolve o áudio ou o erro configurado.
type fakeExtractor struct {
	err      error
	calls    int
	segments int // 0 = um só
}

func (*fakeExtractor) Supports(u *url.URL) bool { return strings.Contains(u.Host, "youtu") }
func (f *fakeExtractor) Audio(context.Context, *url.URL) (extract.AudioFile, error) {
	f.calls++
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

func TestYouTubeTranscribedAndSearchable(t *testing.T) {
	ex := &fakeExtractor{}
	hn := newHarnessAI(t, fakeAI{audio: speech()})
	hn.h.Extractor = ex
	hn.expect("https://youtu.be/abc", "Salvo (#1, youtube, transcrito)")
	hn.expect("/recentes", "Como fazer pão de queijo")
	hn.expect("comida", "#1") // busca semântica sobre título/resumo/tags
	hn.expect("hoje vamos fazer", "#1")
	// não ficou nada em espera: a próxima mensagem sem link é busca
	hn.expect("bola", "Não achei nada")
	// repetido: não baixa de novo
	hn.expect("https://youtu.be/abc", "já está salvo")
	if ex.calls != 1 {
		t.Fatalf("extraiu %d vezes", ex.calls)
	}
}

func TestTranscribeFallbacksAskForDescription(t *testing.T) {
	cases := []struct {
		name string
		ex   *fakeExtractor
		ai   fakeAI
		want string
	}{
		{"longo", &fakeExtractor{err: &extract.TooLongError{Duration: 20 * time.Minute, Max: 10 * time.Minute}}, fakeAI{audio: speech()}, "longo demais (20 min 00 s; o máximo é 10 min 00 s)"},
		{"ao vivo", &fakeExtractor{err: extract.ErrNoDuration}, fakeAI{audio: speech()}, "transmissão ao vivo"},
		{"download falhou", &fakeExtractor{err: errors.New("yt-dlp quebrou")}, fakeAI{audio: speech()}, "não consegui baixar o áudio"},
		{"sem fala", &fakeExtractor{}, fakeAI{audio: ai.Analysis{HasSpeech: false}}, "não tem fala"},
		{"IA caiu", &fakeExtractor{}, fakeAI{down: true}, "a IA não conseguiu transcrever"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hn := newHarnessAI(t, c.ai)
			hn.h.Extractor = c.ex
			hn.expect("https://youtu.be/abc", c.want)
			hn.expect("https://youtu.be/abc", "Me diga do que ele trata")
			// o usuário descreve e o fluxo manual segue normal
			hn.expect("vídeo de receita", "Salvo (#1, youtube)")
		})
	}
}

func TestNoteWithLinkSkipsTranscription(t *testing.T) {
	ex := &fakeExtractor{}
	hn := newHarnessAI(t, fakeAI{audio: speech()})
	hn.h.Extractor = ex
	hn.expect("https://youtu.be/abc minha descrição", "Salvo (#1, youtube)")
	if ex.calls != 0 {
		t.Fatal("não deveria extrair quando há descrição")
	}
}

func TestNoExtractionWithoutAIOrExtractor(t *testing.T) {
	hn := newHarness(t) // IA desligada, extrator Nop
	hn.h.Extractor = &fakeExtractor{}
	hn.expect("https://youtu.be/abc", "sem GEMINI_API_KEY")

	hn2 := newHarnessAI(t, fakeAI{audio: speech()}) // IA ligada, extrator Nop
	hn2.expect("https://youtu.be/abc", "yt-dlp e ffmpeg")
}

func TestLongVideoIsTranscribedInSegments(t *testing.T) {
	ex := &fakeExtractor{segments: 5}
	hn := newHarnessAI(t, fakeAI{audio: speech()})
	hn.h.Extractor = ex
	hn.expect("https://youtu.be/abc", "Resumo geral da transcrição")
	items, _ := hn.h.Store.Recent(context.Background(), 1, 1)
	// 5 trechos iguais concatenados: a transcrição guardada é a completa.
	if got := strings.Count(items[0].Transcript, "hoje vamos fazer pão de queijo"); got != 5 || items[0].Source != "transcript" {
		t.Fatalf("transcrição com %d trechos: %q", got, items[0].Transcript)
	}
}
