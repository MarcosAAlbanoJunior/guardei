package bot

import (
	"context"
	"strings"
	"testing"
	"time"
)

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

// Mensagens do mesmo chat são tratadas uma de cada vez, na ordem em que a trava é obtida.
func TestSameChatMessagesAreSerialized(t *testing.T) {
	hn := newHarness(t)
	inside, release := make(chan struct{}), make(chan struct{})
	hn.h.Reply = func(_ context.Context, _ int64, text string) {
		hn.mu.Lock()
		hn.all = append(hn.all, text)
		hn.mu.Unlock()
		if strings.Contains(text, "Comando desconhecido") { // 1ª mensagem: segura a trava
			close(inside)
			<-release
		}
	}

	done := make(chan struct{})
	go func() { hn.h.Handle(context.Background(), 1, 1, "/xyz"); done <- struct{}{} }()
	<-inside
	go func() { hn.h.Handle(context.Background(), 1, 1, "/status"); done <- struct{}{} }()

	time.Sleep(50 * time.Millisecond)
	hn.mu.Lock()
	n := len(hn.all)
	hn.mu.Unlock()
	if n != 1 {
		t.Fatalf("a 2ª mensagem do mesmo chat rodou antes de a 1ª terminar: %q", hn.all)
	}
	close(release)
	<-done
	<-done
	if len(hn.all) != 2 || !strings.Contains(hn.all[1], "IA:") {
		t.Fatalf("%q", hn.all)
	}
}

// Chats diferentes não se bloqueiam.
func TestDifferentChatsRunConcurrently(t *testing.T) {
	hn := newHarness(t)
	hn.h.Allowed[2] = true
	inside, release := make(chan struct{}), make(chan struct{})
	hn.h.Reply = func(_ context.Context, chatID int64, text string) {
		if chatID == 1 {
			close(inside)
			<-release
		}
	}
	go hn.h.Handle(context.Background(), 1, 1, "/status")
	<-inside
	finished := make(chan struct{})
	go func() { hn.h.Handle(context.Background(), 2, 2, "/status"); close(finished) }()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("o chat 2 ficou preso esperando o chat 1")
	}
	close(release)
}
