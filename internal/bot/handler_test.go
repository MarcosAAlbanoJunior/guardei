package bot

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/marcosjunior/guardei/internal/ai"
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
	hn.h = &Handler{Store: st, AI: client, AIInfo: "fake",
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
type fakeAI struct{ down bool }

func (fakeAI) Enabled() bool { return true }
func (fakeAI) AnalyzeAudio(context.Context, []byte, string) (ai.Analysis, error) {
	return ai.Analysis{}, nil
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
	return []float32{0.1, 0.1}, nil
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
