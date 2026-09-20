// Package bot contém a lógica de conversa do Guardei (uma máquina de estados
// por chat) e a ligação com o Telegram.
package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/MarcosAAlbanoJunior/guardei/internal/ai"
	"github.com/MarcosAAlbanoJunior/guardei/internal/extract"
	"github.com/MarcosAAlbanoJunior/guardei/internal/page"
	"github.com/MarcosAAlbanoJunior/guardei/internal/platform"
	"github.com/MarcosAAlbanoJunior/guardei/internal/search"
	"github.com/MarcosAAlbanoJunior/guardei/internal/store"
)

const (
	recentLimit = 10
	aiTimeout   = 30 * time.Second
)

// Handler processa mensagens já desacopladas do Telegram, o que permite testar
// o fluxo inteiro sem rede. Use-o sempre por ponteiro.
type Handler struct {
	Store       *store.Store
	AI          ai.Client
	Searcher    *search.Searcher
	Extractor   extract.Extractor
	Pages       page.Reader // nil = leitura de posts desligada
	AIInfo      string      // texto do /status, ex.: nomes dos modelos
	Allowed     map[int64]bool
	SearchLimit int

	// Reply envia uma mensagem ao chat. Run o define ao conectar no Telegram.
	Reply func(ctx context.Context, chatID int64, text string)
	// Keyboard envia uma mensagem com botões sob ela. Se nil, os botões são omitidos.
	Keyboard func(ctx context.Context, chatID int64, text string, rows [][]Button)
	// ClearKeyboard tira os botões de uma mensagem já enviada.
	ClearKeyboard func(ctx context.Context, chatID int64, messageID int)

	chats    sync.Map // chatID -> *sync.Mutex
	sessions sessionStore
}

// Handle trata uma mensagem de texto de um usuário.
func (h *Handler) Handle(ctx context.Context, userID, chatID int64, text string) {
	if !h.Allowed[userID] {
		slog.Warn("usuário não autorizado", "user_id", userID)
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}

	// O Telegram entrega cada mensagem em uma goroutine. Sem esta trava, uma
	// descrição enviada logo depois de um link poderia ser tratada antes de a
	// espera ser gravada, e viraria uma busca.
	defer h.lockChat(chatID)()

	if err := h.dispatch(ctx, userID, chatID, text); err != nil {
		h.fail(ctx, chatID, err)
	}
}

// fail registra o erro e avisa o usuário sem expor detalhes.
func (h *Handler) fail(ctx context.Context, chatID int64, err error) {
	slog.Error("erro ao tratar mensagem", "err", err)
	h.Reply(ctx, chatID, "Algo deu errado do meu lado. Tente de novo em instantes.")
}

func (h *Handler) lockChat(chatID int64) (unlock func()) {
	mu, _ := h.chats.LoadOrStore(chatID, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	return mu.(*sync.Mutex).Unlock
}

// dispatch decide o que fazer com a mensagem: comando, link, resposta a um
// pedido de descrição ou busca.
func (h *Handler) dispatch(ctx context.Context, userID, chatID int64, text string) error {
	if strings.HasPrefix(text, "/") {
		return h.command(ctx, userID, chatID, text)
	}
	if link, rest, ok := platform.FindURL(text); ok {
		return h.link(ctx, userID, chatID, link, rest)
	}
	if p, err := h.Store.GetPending(ctx, chatID); err == nil {
		return h.describe(ctx, userID, chatID, p, text)
	}
	return h.search(ctx, userID, chatID, text)
}

func (h *Handler) itemNotFound(ctx context.Context, chatID, id int64) {
	h.Reply(ctx, chatID, fmt.Sprintf("Não achei o item #%d.", id))
}
