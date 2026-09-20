package bot

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MarcosAAlbanoJunior/guardei/internal/ai"
	"github.com/MarcosAAlbanoJunior/guardei/internal/page"
)

func TestInstagramPostReadAndSearchable(t *testing.T) {
	pg := &fakePages{pg: page.Page{Title: "CAT no Instagram", Text: "Post institucional forte é feito de estratégia e posicionamento de marca."}}
	hn := withPages(t, fakeAI{}, pg)
	hn.expect("https://www.instagram.com/p/DUl9Wk2CZoo/?igshid=x", "Salvo (#1, instagram, post)")
	hn.expect("/recentes", "Título IA")
	hn.expect("posicionamento", "#1") // achado pelo texto do post (FTS), sem estar no resumo
	items, _ := hn.h.Store.Recent(context.Background(), 1, 1)
	if items[0].Source != "page" || !strings.Contains(items[0].Transcript, "posicionamento de marca") {
		t.Fatalf("%+v", items[0])
	}
	// nada ficou em espera
	hn.expect("bola", "Não achei nada")
}

func TestLinkedInPostAndOtherSites(t *testing.T) {
	pg := &fakePages{pg: page.Page{Text: "Dez dicas de CSS para quem trabalha com front-end todos os dias."}}
	hn := withPages(t, fakeAI{}, pg)
	hn.expect("https://pt.linkedin.com/posts/fulano_css-activity-1-AbC?utm_source=share", "Salvo (#1, linkedin, post)")
	hn.expect("https://blog.exemplo.com/artigo", "Salvo (#2, other, post)")
	if pg.calls != 2 {
		t.Fatal(pg.calls)
	}
}

func TestPartialXPostWarns(t *testing.T) {
	pg := &fakePages{pg: page.Page{Text: "Thread pra você entender a maior fofoca da bolha tech de 2026. Cada app…", Partial: true}}
	hn := withPages(t, fakeAI{}, pg)
	hn.h.Extractor = &fakeExtractor{err: errors.New("No video could be found in this tweet")} // tweet só de texto
	hn.expect("https://x.com/user/status/1", "Salvo (#1, x, post)")
	hn.expect("/recentes", "Título IA")
	if !strings.Contains(hn.say("https://x.com/user/status/2"), "/editar 2 para completar") {
		t.Fatal("deveria avisar que o texto veio cortado")
	}
}

func TestPostFallbacksAskForDescription(t *testing.T) {
	cases := []struct {
		name string
		pg   *fakePages
		ai   fakeAI
		want string
	}{
		{"bloqueado", &fakePages{err: page.ErrBlocked}, fakeAI{}, "bloqueou o acesso ou pede login"},
		{"sem texto", &fakePages{err: page.ErrNoContent}, fakeAI{}, "não tem texto aproveitável"},
		{"erro de rede", &fakePages{err: errors.New("timeout")}, fakeAI{}, "não consegui abrir a página"},
		{"tela de login", &fakePages{pg: page.Page{Text: "ENTRE OU CADASTRE-SE para ver mais"}}, fakeAI{}, "não descreve o post"},
		{"IA caiu", &fakePages{pg: page.Page{Text: "texto do post com conteúdo suficiente"}}, fakeAI{down: true}, "a IA não conseguiu analisar o post"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hn := withPages(t, c.ai, c.pg)
			hn.expect("https://www.instagram.com/p/abc/", c.want)
			hn.expect("https://www.instagram.com/p/abc/", "Me diga do que ele trata")
			// como no Instagram de sempre: o usuário descreve e o item é salvo
			hn.expect("post sobre estratégia de marca", "Salvo (#1, instagram)")
		})
	}
}

func TestVideoFailureMentionsBothReasons(t *testing.T) {
	pg := &fakePages{err: page.ErrBlocked}
	hn := withPages(t, fakeAI{audio: speech()}, pg)
	hn.h.Extractor = &fakeExtractor{err: errors.New("yt-dlp quebrou")}
	hn.expect("https://youtu.be/abc", "não consegui baixar o áudio; o site bloqueou")
}

func TestVideoLimitsDoNotFallBackToPage(t *testing.T) {
	// vídeo longo, ao vivo e sem fala têm o motivo próprio; não vale ler a página
	pg := &fakePages{pg: page.Page{Text: "descrição comprida do vídeo no youtube com bastante texto"}}
	hn := withPages(t, fakeAI{audio: ai.Analysis{HasSpeech: false}}, pg)
	hn.h.Extractor = &fakeExtractor{}
	hn.expect("https://youtu.be/abc", "não tem fala")
	if pg.calls != 0 {
		t.Fatal("não deveria ler a página")
	}
}

func TestNoPostReadingWithoutAI(t *testing.T) {
	pg := &fakePages{pg: page.Page{Text: "texto do post com conteúdo suficiente"}}
	hn := newHarness(t) // IA desligada
	hn.h.Pages = pg
	hn.expect("https://www.instagram.com/p/abc/", "Me diga do que ele trata")
	if pg.calls != 0 {
		t.Fatal("sem IA não deve nem abrir a página")
	}
}

func TestNoteWithLinkNeverReadsPage(t *testing.T) {
	pg := &fakePages{pg: page.Page{Text: "texto do post com conteúdo suficiente"}}
	hn := withPages(t, fakeAI{}, pg)
	hn.expect("https://www.instagram.com/p/abc/ minha descrição", "Salvo (#1, instagram)")
	if pg.calls != 0 {
		t.Fatal("com descrição na mensagem não deve ler a página")
	}
}
