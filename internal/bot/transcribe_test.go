package bot

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MarcosAAlbanoJunior/guardei/internal/ai"
	"github.com/MarcosAAlbanoJunior/guardei/internal/extract"
	"github.com/MarcosAAlbanoJunior/guardei/internal/page"
)

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
	items := hn.newest(1)
	// 5 trechos iguais concatenados: a transcrição guardada é a completa.
	if got := strings.Count(items[0].Transcript, "hoje vamos fazer pão de queijo"); got != 5 || items[0].Source != "transcript" {
		t.Fatalf("transcrição com %d trechos: %q", got, items[0].Transcript)
	}
}

// Post de texto no X: não há vídeo, então o usuário nunca vê "transcrevendo…".
func TestTextPostNeverShowsDownloadMessage(t *testing.T) {
	pg := &fakePages{pg: page.Page{Text: "Um post de texto puro no X, sem nenhum vídeo anexado a ele."}}
	hn := withPages(t, fakeAI{}, pg)
	hn.h.Extractor = &fakeExtractor{err: errors.New("No video could be found in this tweet")}
	hn.expect("https://x.com/user/status/1", "Salvo (#1, x, post)")
	if hn.said("transcrevendo") || hn.said("Baixando") {
		t.Fatalf("mostrou aviso de vídeo em post de texto: %q", hn.all)
	}
	if !hn.said("Lendo o post") {
		t.Fatalf("deveria avisar que está lendo o post: %q", hn.all)
	}
}

// Vídeo de verdade: o aviso aparece, uma vez, antes do resultado.
func TestVideoShowsDownloadMessageOnce(t *testing.T) {
	hn := newHarnessAI(t, fakeAI{audio: speech()})
	hn.h.Extractor = &fakeExtractor{}
	hn.expect("https://youtu.be/abc", "transcrito")
	n := 0
	for _, m := range hn.all {
		if strings.Contains(m, "Baixando o áudio") {
			n++
		}
	}
	if n != 1 || !strings.Contains(hn.all[0], "Baixando o áudio") {
		t.Fatalf("aviso deveria vir 1 vez e primeiro: %q", hn.all)
	}
}

// Vídeo existe mas o download falha: o aviso saiu, depois vem a leitura do post.
func TestVideoDownloadFailureThenReadsPost(t *testing.T) {
	pg := &fakePages{pg: page.Page{Text: "Descrição do post com texto suficiente para a análise da IA."}}
	hn := withPages(t, fakeAI{audio: speech()}, pg)
	hn.h.Extractor = &fakeExtractor{err: errors.New("yt-dlp quebrou"), startedFirst: true}
	hn.expect("https://x.com/user/status/1", "Salvo (#1, x, post)")
	if !hn.said("Baixando o áudio") || !hn.said("Lendo o post") {
		t.Fatalf("%q", hn.all)
	}
}

// Receita completa: título e descrição têm bastante texto para o bot entender o vídeo.
func recipeMeta() extract.Meta {
	return extract.Meta{
		Title:       "Pão de queijo com polvilho azedo",
		Description: "Ingredientes: 500g de polvilho azedo, 3 ovos e 300g de queijo ralado. Modo de preparo no vídeo.",
		Uploader:    "Ediane - Comida Mineira",
		Tags:        []string{"receita", "pão de queijo"},
		Categories:  []string{"Howto & Style"},
		Duration:    4 * time.Minute,
	}
}

// Sem fala, longo demais, ao vivo, áudio grande ou falha da IA: em vez de pedir
// descrição, o bot usa o título e a descrição do vídeo.
func TestVideoWithoutUsableAudioFallsBackToTitleAndDescription(t *testing.T) {
	cases := []struct {
		name string
		ex   *fakeExtractor
		ai   fakeAI
		why  string
	}{
		{"sem fala", &fakeExtractor{}, fakeAI{audio: ai.Analysis{HasSpeech: false}}, "O vídeo não tem fala"},
		{"longo demais", &fakeExtractor{err: &extract.TooLongError{Duration: 25 * time.Minute, Max: 10 * time.Minute}}, fakeAI{audio: speech()}, "O vídeo é longo demais"},
		{"ao vivo", &fakeExtractor{err: extract.ErrNoDuration}, fakeAI{audio: speech()}, "Não consegui saber a duração"},
		{"áudio grande", &fakeExtractor{err: extract.ErrTooLarge}, fakeAI{audio: speech()}, "O áudio ficou grande demais"},
		{"IA não transcreveu", &fakeExtractor{}, fakeAI{audio: speech(), noAudio: true}, "A IA não conseguiu transcrever"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pg := &fakePages{err: page.ErrBlocked}
			c.ex.meta = recipeMeta()
			hn := withPages(t, c.ai, pg)
			hn.h.Extractor = c.ex

			hn.expect("https://youtu.be/abc", "Salvo (#1, youtube, por título e descrição)")
			if !hn.said("⏳ "+c.why) || !hn.said("Vou usar o título e a descrição") {
				t.Errorf("deveria explicar o motivo antes de usar os metadados: %q", hn.all)
			}
			if pg.calls != 0 || c.ex.metaCalls != 1 {
				t.Errorf("metadados bastam: páginas=%d metadados=%d", pg.calls, c.ex.metaCalls)
			}
			it := hn.newest(1)[0]
			if it.Source != "metadata" || it.Title != "Pão de queijo com polvilho azedo" || !strings.Contains(it.Transcript, "300g de queijo ralado") ||
				!strings.Contains(it.Transcript, "Tags do vídeo: receita, pão de queijo") || it.Summary == "" || len(it.Tags) == 0 {
				t.Fatalf("%+v", it)
			}
			// a descrição entra no índice: acha por um ingrediente que não está no título
			hn.expect("polvilho", "#1")
			hn.expect("ovos", "#1")
			// nada ficou em espera
			hn.expect("bola", "Não achei nada")
		})
	}
}

// A IA recebe canal, título, descrição e tags, e sabe que é um vídeo sem transcrição.
func TestVideoMetadataIsWhatTheAIAnalyzes(t *testing.T) {
	var got ai.Post
	client := postSpy{fakeAI: fakeAI{audio: ai.Analysis{HasSpeech: false}}, seen: &got}
	hn := newHarnessAI(t, client)
	hn.h.Extractor = &fakeExtractor{meta: recipeMeta()}
	hn.expect("https://youtu.be/abc", "por título e descrição")
	if got.Title != "Pão de queijo com polvilho azedo" || got.Author != "Ediane - Comida Mineira" || got.Platform != "youtube" ||
		!strings.Contains(got.Text, "500g de polvilho azedo") || !strings.Contains(got.Text, "Categoria: Howto & Style") {
		t.Fatalf("%+v", got)
	}
}

func TestThinMetadataFallsBackToThePage(t *testing.T) {
	pg := &fakePages{pg: page.Page{Text: "Texto da página do vídeo com o bastante para a análise da IA seguir."}}
	hn := withPages(t, fakeAI{audio: ai.Analysis{HasSpeech: false}}, pg)
	hn.h.Extractor = &fakeExtractor{meta: extract.Meta{Title: "Música", Description: ""}} // 6 caracteres: pouco
	hn.expect("https://youtu.be/abc", "Salvo (#1, youtube, post)")
	if pg.calls != 1 {
		t.Fatal("com metadados curtos, deveria ler a página")
	}

	hn2 := withPages(t, fakeAI{audio: ai.Analysis{HasSpeech: false}}, &fakePages{pg: page.Page{Text: "Texto da página do vídeo com o bastante para a análise."}})
	hn2.h.Extractor = &fakeExtractor{metaErr: errors.New("yt-dlp bloqueado")}
	hn2.expect("https://youtu.be/abc", "Salvo (#1, youtube, post)")
}

func TestUselessMetadataAndPageAskForDescriptionWithBothReasons(t *testing.T) {
	// metadados existem, mas a IA vê que não dizem nada (ex.: só "Video 001")
	hn := withPages(t, fakeAI{audio: ai.Analysis{HasSpeech: false}}, &fakePages{err: page.ErrBlocked})
	hn.h.Extractor = &fakeExtractor{meta: extract.Meta{Title: "ENTRE OU CADASTRE-SE", Description: "ENTRE OU CADASTRE-SE para ver mais"}}
	got := hn.say("https://youtu.be/abc")
	if !strings.Contains(got, "o que consegui ler não descreve o conteúdo") || !strings.Contains(got, "Me diga do que ele trata") {
		t.Fatalf("%s", got)
	}
	hn.expect("vídeo de música", "Salvo (#1, youtube)") // o fluxo manual continua
}

// Sem IA, nada disso acontece: continua pedindo a descrição.
func TestNoMetadataFallbackWithoutAI(t *testing.T) {
	ex := &fakeExtractor{meta: recipeMeta()}
	hn := newHarness(t)
	hn.h.Extractor = ex
	hn.expect("https://youtu.be/abc", "sem GEMINI_API_KEY")
	if ex.metaCalls != 0 {
		t.Fatal("sem IA não há o que analisar")
	}
}

// Download bloqueado (ex.: HTTP 403 do YouTube): o vídeo existe, então a mensagem
// fala em título e descrição; no X, sem vídeo, é "lendo o post".
func TestDownloadFailureWording(t *testing.T) {
	meta := &fakeExtractor{err: errors.New("yt-dlp: HTTP Error 403: Forbidden"), startedFirst: true, meta: recipeMeta()}
	hn := withPages(t, fakeAI{audio: speech()}, &fakePages{err: page.ErrBlocked})
	hn.h.Extractor = meta
	hn.expect("https://youtu.be/abc", "Salvo (#1, youtube, por título e descrição)")
	if !hn.said("⏳ Não consegui baixar o áudio. Vou usar o título e a descrição.") {
		t.Fatalf("%q", hn.all)
	}

	tw := withPages(t, fakeAI{audio: speech()}, &fakePages{pg: page.Page{Text: "Um post de texto puro no X, sem nenhum vídeo anexado a ele."}})
	tw.h.Extractor = &fakeExtractor{err: errors.New("No video could be found in this tweet")}
	tw.expect("https://x.com/u/status/1", "Salvo (#1, x, post)")
	if tw.said("título e a descrição") || !tw.said("Lendo o post") {
		t.Fatalf("%q", tw.all)
	}
}
