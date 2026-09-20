package search

import "testing"

func TestBuildFTSQuery(t *testing.T) {
	got := BuildFTSQuery(`Receita de "pão" de queijo!! a`)
	want := `"receita"* OR "de"* OR "pão"* OR "queijo"*`
	if got != want {
		t.Errorf("got %s", got)
	}
	if BuildFTSQuery("? ! a") != "" {
		t.Error("esperava vazio")
	}
}
