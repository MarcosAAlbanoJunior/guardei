package bot

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSearchShowsTotalAndLoadsMoreOnDemand(t *testing.T) {
	hn := newHarness(t) // sem IA: só FTS
	ids := hn.seed("example.com", "receita de queijo", 12)

	first := hn.say("receita")
	if !strings.Contains(first, "Achei 12 itens · mostrando 1–5:") || itemsIn(first) != 5 {
		t.Fatalf("primeira página:\n%s", first)
	}
	if !hn.hasButton("Ver mais 5") || !hn.hasButton("Hoje") || !hn.hasButton("7 dias") || !hn.hasButton("30 dias") {
		t.Fatalf("botões: %+v", hn.buttons)
	}
	more := hn.button("Ver mais")

	second := hn.tapOn(10, more)
	if !strings.Contains(second, "Resultados 6–10 de 12:") || itemsIn(second) != 5 || !hn.hasButton("Ver mais 2") {
		t.Fatalf("segunda página:\n%s\n%+v", second, hn.buttons)
	}
	if len(hn.cleared) != 1 || hn.cleared[0] != 10 {
		t.Fatalf("o botão usado deveria sair da mensagem anterior: %v", hn.cleared)
	}

	third := hn.tapOn(11, hn.button("Ver mais"))
	if !strings.Contains(third, "Resultados 11–12 de 12:") || itemsIn(third) != 2 || hn.hasButton("Ver mais") {
		t.Fatalf("última página:\n%s\n%+v", third, hn.buttons)
	}

	// as três páginas cobrem os 12 itens, sem repetir
	seen := map[string]bool{}
	for _, page := range []string{first, second, third} {
		for _, id := range ids {
			if strings.Contains(page, "\n#"+itoa(id)+" ·") {
				if seen[itoa(id)] {
					t.Errorf("item #%d apareceu duas vezes", id)
				}
				seen[itoa(id)] = true
			}
		}
	}
	if len(seen) != 12 {
		t.Errorf("as páginas cobrem %d de 12 itens", len(seen))
	}
}

func TestDoubleTapAndStaleButtonsDoNothing(t *testing.T) {
	hn := newHarness(t)
	hn.seed("example.com", "receita de queijo", 8)
	hn.say("receita")
	more := hn.button("Ver mais")

	if hn.tapOn(7, more) == "" {
		t.Fatal("o primeiro toque deveria mostrar a próxima página")
	}
	if got := hn.tapOn(7, more); got != "" { // mesmo botão, mesma mensagem
		t.Fatalf("toque duplo repetiu a página: %q", got)
	}

	// uma busca nova troca a sessão: o botão da busca anterior expira
	hn.say("queijo")
	if got := hn.tapOn(8, more); !strings.Contains(got, "expirou") {
		t.Fatalf("botão de busca antiga: %q", got)
	}
}

func TestExpiredSession(t *testing.T) {
	hn := newHarness(t)
	hn.seed("example.com", "receita de queijo", 8)
	hn.say("receita")
	more := hn.button("Ver mais")
	hn.h.sessions.m[1].created = time.Now().Add(-sessionTTL - time.Minute)

	if got := hn.tapOn(3, more); !strings.Contains(got, "expirou") {
		t.Fatalf("%q", got)
	}
	if len(hn.cleared) != 1 { // os botões mortos saem da mensagem
		t.Fatalf("%v", hn.cleared)
	}
}

func TestCallbackFromUnauthorizedUserIsIgnored(t *testing.T) {
	hn := newHarness(t)
	hn.seed("example.com", "receita de queijo", 8)
	hn.say("receita")
	hn.last = ""
	hn.h.HandleCallback(context.Background(), 99, 1, 5, hn.button("Ver mais"))
	if hn.last != "" {
		t.Fatalf("respondeu a usuário não autorizado: %q", hn.last)
	}
	// e botões desconhecidos ou de outra versão não quebram nada
	for _, data := range []string{"", "x", "m:abc", "z:1", "p:1:x"} {
		hn.tap(data)
	}
}

// "Ver mais" e os botões de período não repetem a busca nem a chamada à IA.
func TestMoreAndPeriodButtonsDoNotCallTheAIAgain(t *testing.T) {
	embeds := 0
	hn := newHarnessAI(t, fakeAI{embeds: &embeds})
	ids := hn.seed("example.com", "pão de queijo", 12)
	for i, id := range ids { // 3 de hoje, 3 de 3 dias, 3 de 20 dias, 3 de 60 dias
		hn.backdate(id, []time.Duration{0, 3 * 24 * time.Hour, 20 * 24 * time.Hour, 60 * 24 * time.Hour}[i/3])
	}

	embeds = 0
	first := hn.say("comida") // "comida" não está em nenhum texto: só a busca por sentido acha
	if !strings.Contains(first, "Achei 12 itens · mostrando 1–5:") || embeds != 1 {
		t.Fatalf("embeds=%d\n%s", embeds, first)
	}
	hn.tap(hn.button("Ver mais"))

	for _, c := range []struct {
		button string
		want   string
		count  int
	}{
		{"Hoje", "Achei 3 itens · mostrando 1–3:\nFiltro: hoje", 3},
		{"7 dias", "Achei 6 itens · mostrando 1–5:\nFiltro: últimos 7 dias", 5},
		{"30 dias", "Achei 9 itens · mostrando 1–5:\nFiltro: últimos 30 dias", 5},
	} {
		hn.say("comida")
		got := hn.tap(hn.button(c.button))
		if !strings.Contains(got, c.want) || itemsIn(got) != c.count {
			t.Errorf("%s:\n%s", c.button, got)
		}
		if !hn.hasButton("Todo o período") {
			t.Errorf("%s: deveria oferecer voltar ao período todo", c.button)
		}
	}
	// 1 busca por rodada de "comida" acima (3 rodadas) + a inicial; os toques não chamaram a IA
	if embeds != 4 {
		t.Fatalf("Embed chamado %d vezes; esperado 4 (uma por busca digitada, nenhuma por toque)", embeds)
	}

	hn.say("comida")
	hn.tap(hn.button("Hoje"))
	if all := hn.tap(hn.button("Todo o período")); !strings.Contains(all, "Achei 12 itens") || strings.Contains(all, "Filtro") {
		t.Fatalf("todo o período:\n%s", all)
	}
}

func TestFiltersWrittenInTheSearchText(t *testing.T) {
	hn := newHarness(t) // sem IA: os filtros funcionam igual
	yt := hn.seed("youtu.be", "receita de queijo", 2)
	tt := hn.seed("tiktok.com/@u/video", "receita de queijo", 2)
	hn.backdate(yt[0], 20*24*time.Hour)
	hn.backdate(tt[0], 3*24*time.Hour)

	if got := hn.say("queijo da semana"); !strings.Contains(got, "Filtro: últimos 7 dias") || itemsIn(got) != 3 {
		t.Fatalf("da semana:\n%s", got)
	}
	if got := hn.say("queijo de hoje"); !strings.Contains(got, "Filtro: hoje") || itemsIn(got) != 2 {
		t.Fatalf("hoje:\n%s", got)
	}
	if got := hn.say("queijo do youtube"); !strings.Contains(got, "Filtro: YouTube") || itemsIn(got) != 2 {
		t.Fatalf("plataforma:\n%s", got)
	}
	if got := hn.say("queijo do youtube hoje"); !strings.Contains(got, "Filtro: YouTube · hoje") || itemsIn(got) != 1 {
		t.Fatalf("plataforma e período:\n%s", got)
	}

	// só filtro, sem assunto: lista do mais novo ao mais antigo
	got := hn.say("o que eu salvei hoje")
	if !strings.Contains(got, "2 itens, do mais novo ao mais antigo · mostrando 1–2:") || itemsIn(got) != 2 {
		t.Fatalf("só filtro:\n%s", got)
	}
	if strings.Index(got, "#"+itoa(tt[1])) > strings.Index(got, "#"+itoa(yt[1])) {
		t.Errorf("deveria vir do mais novo ao mais antigo:\n%s", got)
	}
	if got := hn.say("só do tiktok"); itemsIn(got) != 2 || !strings.Contains(got, "Filtro: TikTok") {
		t.Fatalf("só plataforma:\n%s", got)
	}

	// sem resultado: diz o filtro e oferece voltar ao período todo
	got = hn.say("queijo de ontem")
	if !strings.Contains(got, "Não achei nada (filtro: ontem)") || !hn.hasButton("Todo o período") {
		t.Fatalf("%q %+v", got, hn.buttons)
	}
	if got := hn.tap(hn.button("Todo o período")); !strings.Contains(got, "Achei 4 itens") {
		t.Fatalf("voltar ao período todo:\n%s", got)
	}
}

func TestRecentesListsNewestFirstAndFiltersByTerm(t *testing.T) {
	hn := newHarness(t)
	ids := hn.seed("example.com", "receita de queijo", 12)
	hn.seed("example.com", "treino de perna", 1)

	got := hn.say("/recentes")
	if !strings.Contains(got, "13 itens, do mais novo ao mais antigo · mostrando 1–10:") || itemsIn(got) != 10 || !hn.hasButton("Ver mais 3") {
		t.Fatalf("%s\n%+v", got, hn.buttons)
	}
	if strings.Index(got, "treino de perna") > strings.Index(got, "receita de queijo") {
		t.Errorf("o mais novo deveria vir primeiro:\n%s", got)
	}

	got = hn.say("/recentes receita")
	if !strings.Contains(got, "12 itens") || itemsIn(got) != 10 || strings.Contains(got, "treino") {
		t.Fatalf("/recentes com termo:\n%s", got)
	}
	if strings.Index(got, "#"+itoa(ids[11])) > strings.Index(got, "#"+itoa(ids[0])) {
		t.Errorf("com termo, ainda do mais novo ao mais antigo:\n%s", got)
	}

	if got := hn.say("/recentes hoje"); !strings.Contains(got, "Filtro: hoje") {
		t.Fatalf("%s", got)
	}
}

// Quando o primeiro vale bem mais que o segundo, só ele aparece; "Ver mais" traz o resto.
func TestDominantResultShownAloneThenMore(t *testing.T) {
	hn := newHarnessAI(t, fakeAI{})
	hn.expect("https://example.com/1 pão de queijo mineiro", "Salvo") // casa no texto e no sentido
	for i := 2; i <= 6; i++ {
		hn.expect("https://example.com/"+itoa(int64(i))+" comida caseira "+itoa(int64(i)), "Salvo") // só no sentido
	}
	got := hn.say("queijo")
	if !strings.Contains(got, "Achei 6 itens · mostrando 1:") || itemsIn(got) != 1 || !strings.Contains(got, "queijo") {
		t.Fatalf("dominante:\n%s", got)
	}
	if !hn.hasButton("Ver mais 5") {
		t.Fatalf("%+v", hn.buttons)
	}
	if rest := hn.tap(hn.button("Ver mais")); !strings.Contains(rest, "Resultados 2–6 de 6:") || itemsIn(rest) != 5 {
		t.Fatalf("resto:\n%s", rest)
	}
}

func TestSingleAndEmptyResults(t *testing.T) {
	hn := newHarness(t)
	hn.expect("/recentes", "Ainda não há itens salvos")
	hn.expect("bolo", "Não achei nada. Tente termos mais gerais.")
	hn.expect("https://example.com/a bolo de cenoura", "Salvo")
	got := hn.say("bolo")
	if !strings.Contains(got, "Achei 1 item:") || itemsIn(got) != 1 || hn.hasButton("Ver mais") || hn.buttons != nil {
		t.Fatalf("um só resultado, sem botões:\n%s\n%+v", got, hn.buttons)
	}
}

func TestWithoutKeyboardSupportButtonsAreOmitted(t *testing.T) {
	hn := newHarness(t)
	hn.seed("example.com", "receita de queijo", 8)
	hn.h.Keyboard = nil
	if got := hn.say("receita"); !strings.Contains(got, "Achei 8 itens") || hn.buttons != nil {
		t.Fatalf("%s %+v", got, hn.buttons)
	}
}
