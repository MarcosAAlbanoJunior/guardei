package search

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/MarcosAAlbanoJunior/guardei/internal/ai"
	"github.com/MarcosAAlbanoJunior/guardei/internal/store"
)

func TestBuildFTSQuery(t *testing.T) {
	got := BuildFTSQuery(`Receita de "pão" de queijo!! a`)
	want := `"receita"* OR "pão"* OR "queijo"*`
	if got != want {
		t.Errorf("got %s", got)
	}
	if got := BuildFTSQuery("dicas para aprender a programar em casa"); got != `"dica"* OR "aprender"* OR "programar"* OR "casa"*` {
		t.Errorf("stopwords: %s", got)
	}
	for in, want := range map[string]string{
		"receitas": `"receita"*`, "videos de bolos": `"bolo"*`, "flores": `"flor"*`, "css": `"css"*`, "pão": `"pão"*`, "ônibus": `"ônibu"*`,
	} {
		if got := BuildFTSQuery(in); got != want {
			t.Errorf("%q -> %s (queria %s)", in, got, want)
		}
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
	got := ix.Nearest(1, []float32{1, 0}, 5, 0.5, nil)
	if len(got) != 2 || got[0].ID != 10 || got[1].ID != 11 {
		t.Fatalf("%+v", got)
	}
	if math.Abs(got[0].Cosine-1) > 1e-9 {
		t.Fatalf("cosseno %v", got[0].Cosine)
	}
	// com filtro, só os ids permitidos
	only11 := map[int64]struct{}{11: {}}
	if got := ix.Nearest(1, []float32{1, 0}, 5, 0.5, only11); len(got) != 1 || got[0].ID != 11 {
		t.Fatalf("allow: %+v", got)
	}
	if got := ix.Nearest(1, []float32{1, 0}, 5, 0.5, map[int64]struct{}{}); len(got) != 0 {
		t.Fatalf("allow vazio deve excluir tudo: %+v", got)
	}
}

func TestDominant(t *testing.T) {
	if !Dominant([]float64{3, 1}) || Dominant([]float64{3, 2.5}) || Dominant([]float64{3}) || Dominant(nil) || Dominant([]float64{3, 0}) {
		t.Fatal("Dominant")
	}
}

func TestWithinMargin(t *testing.T) {
	ns := []Neighbor{{1, 0.75}, {2, 0.70}, {3, 0.681}, {4, 0.60}, {5, 0.55}}
	got := withinMargin(ns)
	if len(got) != 3 || got[2].ID != 3 { // 0,75 - 0,07 = 0,68
		t.Fatalf("%+v", got)
	}
	if len(withinMargin(nil)) != 0 {
		t.Fatal("vazio")
	}
}

// fakeAI: dois "conceitos" — dimensão 0 = comida, 1 = esporte. Conta as chamadas de Embed.
type fakeAI struct {
	fail  bool
	calls *int
}

func (fakeAI) Enabled() bool { return true }
func (fakeAI) AnalyzeAudio(context.Context, []byte, string) (ai.Analysis, error) {
	return ai.Analysis{}, nil
}
func (fakeAI) AnalyzeText(context.Context, string) (ai.Analysis, error)  { return ai.Analysis{}, nil }
func (fakeAI) AnalyzePost(context.Context, ai.Post) (ai.Analysis, error) { return ai.Analysis{}, nil }
func (fakeAI) AnalyzeTranscript(context.Context, string) (ai.Analysis, error) {
	return ai.Analysis{}, nil
}
func (f fakeAI) Embed(_ context.Context, text string, _ ai.EmbedTask) ([]float32, error) {
	if f.calls != nil {
		*f.calls++
	}
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

type fixture struct {
	s          *Searcher
	pao, perna int64
	pastel     int64 // receita antiga do tiktok
}

func newFixture(t *testing.T, client ai.Client) fixture {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir()+"/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ix := NewIndex()
	add := func(key, platform, note string, v []float32, age time.Duration) int64 {
		it := store.Item{UserID: 1, URL: key, CanonicalURL: key, Platform: platform, UserNote: note, Source: "manual"}
		id, _ := st.Insert(ctx, &it)
		st.SetEmbedding(ctx, 1, id, v)
		ix.Set(1, id, v)
		return id
	}
	f := fixture{}
	f.pao = add("a", "youtube", "pão de queijo", []float32{1, 0}, 0)
	f.perna = add("b", "youtube", "treino de perna", []float32{0, 1}, 0)
	f.pastel = add("c", "tiktok", "receita de pastel", []float32{0.95, 0.05}, 0)
	f.s = &Searcher{Store: st, AI: client, Index: ix}
	return f
}

func TestRankHybridFindsBySemanticsAndFallsBackToFTS(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, fakeAI{})

	// "comida" não aparece em nenhum texto: só a busca semântica acha, e os dois
	// itens de comida ficam antes do treino (que nem chega ao corte de relevância).
	rk, err := f.s.Rank(ctx, 1, Query{Text: "comida"}, RankOpts{})
	if err != nil || len(rk.IDs) != 2 || rk.IDs[0] != f.pao || rk.IDs[1] != f.pastel {
		t.Fatalf("semântica: %+v %v", rk, err)
	}
	if len(rk.QueryVec) == 0 || len(rk.Scores) != len(rk.IDs) {
		t.Fatalf("faltou o vetor da consulta ou as pontuações: %+v", rk)
	}

	// embedding falhou: cai no FTS sem erro
	f.s.AI = fakeAI{fail: true}
	rk, err = f.s.Rank(ctx, 1, Query{Text: "perna"}, RankOpts{})
	if err != nil || len(rk.IDs) != 1 || rk.IDs[0] != f.perna {
		t.Fatalf("fallback: %+v %v", rk, err)
	}
	if rk, _ = f.s.Rank(ctx, 1, Query{Text: "comida"}, RankOpts{}); len(rk.IDs) != 0 {
		t.Fatalf("sem semântica não deveria achar: %+v", rk)
	}
}

func TestRankPluralFindsSingularWithoutAI(t *testing.T) {
	f := newFixture(t, ai.New("", "", "")) // sem IA: só FTS
	rk, err := f.s.Rank(context.Background(), 1, Query{Text: "receitas"}, RankOpts{})
	if err != nil || len(rk.IDs) != 1 || rk.IDs[0] != f.pastel {
		t.Fatalf("%+v %v", rk, err)
	}
}

func TestRankReusesQueryVectorAndAppliesFilterToSemantics(t *testing.T) {
	ctx := context.Background()
	calls := 0
	f := newFixture(t, fakeAI{calls: &calls})

	rk, _ := f.s.Rank(ctx, 1, Query{Text: "comida"}, RankOpts{})
	if calls != 1 || len(rk.IDs) != 2 {
		t.Fatalf("calls=%d %+v", calls, rk)
	}
	// mesma consulta com filtro de plataforma: reaproveita o vetor, sem nova chamada à IA
	q := Query{Text: "comida"}
	q.Filter.Platform = "tiktok"
	rk2, err := f.s.Rank(ctx, 1, q, RankOpts{QueryVec: rk.QueryVec})
	if err != nil || calls != 1 {
		t.Fatalf("não deveria chamar a IA de novo: calls=%d %v", calls, err)
	}
	if len(rk2.IDs) != 1 || rk2.IDs[0] != f.pastel {
		t.Fatalf("filtro de plataforma na busca semântica: %+v", rk2)
	}
}

func TestRankWithoutTextListsNewestFirst(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, ai.New("", "", ""))
	f.s.Store.SetEmbedding(ctx, 1, f.pao, nil) // irrelevante; só garante o banco em uso

	q := Query{}
	q.Filter.Platform = "youtube"
	rk, err := f.s.Rank(ctx, 1, q, RankOpts{})
	if err != nil || len(rk.IDs) != 2 || rk.IDs[0] != f.perna || rk.IDs[1] != f.pao { // mais novo primeiro
		t.Fatalf("%+v %v", rk, err)
	}
	if rk.Scores != nil {
		t.Error("ordem por data não tem pontuação")
	}
}

func TestRankNewestReordersRelevantSet(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, fakeAI{})
	rk, err := f.s.Rank(ctx, 1, Query{Text: "comida"}, RankOpts{Newest: true})
	if err != nil || len(rk.IDs) != 2 || rk.IDs[0] != f.pastel || rk.IDs[1] != f.pao {
		t.Fatalf("%+v %v", rk, err)
	}
}

func TestIndexLoadFromStore(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir()+"/t.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a, _ := st.Insert(ctx, &store.Item{UserID: 1, URL: "a", CanonicalURL: "a", Platform: "x", Source: "manual"})
	st.Insert(ctx, &store.Item{UserID: 1, URL: "b", CanonicalURL: "b", Platform: "x", Source: "manual"}) // sem vetor
	st.SetEmbedding(ctx, 1, a, []float32{1, 0})

	ix := NewIndex()
	if err := ix.Load(ctx, st); err != nil {
		t.Fatal(err)
	}
	if ix.Len() != 1 {
		t.Fatalf("len=%d", ix.Len())
	}
	if got := ix.Nearest(1, []float32{1, 0}, 5, 0.9, nil); len(got) != 1 || got[0].ID != a {
		t.Fatalf("%+v", got)
	}
	ix.Remove(a)
	if ix.Len() != 0 {
		t.Fatal("Remove não removeu")
	}
}
