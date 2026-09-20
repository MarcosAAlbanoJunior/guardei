package search

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/marcosjunior/guardei/internal/ai"
	"github.com/marcosjunior/guardei/internal/store"
)

func TestBuildFTSQuery(t *testing.T) {
	got := BuildFTSQuery(`Receita de "pão" de queijo!! a`)
	want := `"receita"* OR "pão"* OR "queijo"*`
	if got != want {
		t.Errorf("got %s", got)
	}
	if got := BuildFTSQuery("dicas para aprender a programar em casa"); got != `"dicas"* OR "aprender"* OR "programar"* OR "casa"*` {
		t.Errorf("stopwords: %s", got)
	}
	if BuildFTSQuery("? ! a o que") != "" {
		t.Error("esperava vazio")
	}
}

func TestNearest(t *testing.T) {
	ix := NewIndex()
	ix.Set(1, 10, []float32{1, 0})
	ix.Set(1, 11, []float32{0.9, 0.1})
	ix.Set(1, 12, []float32{0, 1})
	ix.Set(2, 13, []float32{1, 0}) // outro usuário
	got := ix.Nearest(1, []float32{1, 0}, 5, 0.5)
	if len(got) != 2 || got[0].ID != 10 || got[1].ID != 11 {
		t.Fatalf("%+v", got)
	}
	if math.Abs(got[0].Cosine-1) > 1e-9 {
		t.Fatalf("cosseno %v", got[0].Cosine)
	}
}

func TestPick(t *testing.T) {
	r := func(s ...float64) []Result {
		var out []Result
		for _, x := range s {
			out = append(out, Result{Score: x})
		}
		return out
	}
	if len(Pick(r(3, 1))) != 1 || len(Pick(r(3, 2.5))) != 2 || len(Pick(r(3))) != 1 {
		t.Fatal("Pick")
	}
}

// fakeAI: dois "conceitos" — dimensão 0 = comida, 1 = esporte.
type fakeAI struct{ fail bool }

func (fakeAI) Enabled() bool { return true }
func (fakeAI) AnalyzeAudio(context.Context, []byte, string) (ai.Analysis, error) {
	return ai.Analysis{}, nil
}
func (fakeAI) AnalyzeText(context.Context, string) (ai.Analysis, error) { return ai.Analysis{}, nil }
func (fakeAI) AnalyzeTranscript(context.Context, string) (ai.Analysis, error) {
	return ai.Analysis{}, nil
}
func (f fakeAI) Embed(_ context.Context, text string, _ ai.EmbedTask) ([]float32, error) {
	if f.fail {
		return nil, errors.New("falhou")
	}
	switch text {
	case "comida":
		return []float32{1, 0}, nil
	case "esporte":
		return []float32{0, 1}, nil
	}
	return []float32{0.1, 0.1}, nil
}

func TestHybridFindsBySemanticsAndFallsBackToFTS(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir()+"/t.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	idPao, _ := st.Insert(ctx, &store.Item{UserID: 1, URL: "a", CanonicalURL: "a", Platform: "other", UserNote: "pão de queijo", Source: "manual"})
	idTreino, _ := st.Insert(ctx, &store.Item{UserID: 1, URL: "b", CanonicalURL: "b", Platform: "other", UserNote: "treino de perna", Source: "manual"})
	ix := NewIndex()
	ix.Set(1, idPao, []float32{1, 0})
	ix.Set(1, idTreino, []float32{0, 1})

	s := &Searcher{Store: st, AI: fakeAI{}, Index: ix}

	// "comida" não aparece em nenhum texto: só a busca semântica acha.
	got, err := s.Search(ctx, 1, "comida", 5)
	if err != nil || len(got) != 1 || got[0].Item.ID != idPao {
		t.Fatalf("semântica: %+v %v", got, err)
	}

	// embedding falhou: cai no FTS sem erro.
	s.AI = fakeAI{fail: true}
	got, err = s.Search(ctx, 1, "perna", 5)
	if err != nil || len(got) != 1 || got[0].Item.ID != idTreino {
		t.Fatalf("fallback: %+v %v", got, err)
	}
	if got, _ = s.Search(ctx, 1, "comida", 5); len(got) != 0 {
		t.Fatalf("sem semântica não deveria achar: %+v", got)
	}
}
