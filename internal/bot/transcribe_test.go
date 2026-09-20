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
