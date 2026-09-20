package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MarcosAAlbanoJunior/guardei/internal/ai"
	"github.com/MarcosAAlbanoJunior/guardei/internal/extract"
	"github.com/MarcosAAlbanoJunior/guardei/internal/page"
	"github.com/MarcosAAlbanoJunior/guardei/internal/platform"
	"github.com/MarcosAAlbanoJunior/guardei/internal/store"
)

const (
	pageTimeout = 30 * time.Second
	// minMetaRunes: título e descrição menores que isso não dão o que analisar.
	minMetaRunes = 25
	maxMetaText  = 6000
)

func (h *Handler) pages() page.Reader {
	if h.Pages == nil {
		return page.Nop{}
	}
	return h.Pages
}

// content é o texto lido de um link, para a IA analisar.
type content struct {
	page.Page
	videoMeta bool // veio dos metadados do vídeo, não da página
}

// readPost salva o link pelo texto que ele tem e pede à IA título, resumo e tags:
// título e descrição do vídeo quando o áudio não serviu, ou o texto do post.
// Devolve "" quando salvou, ou o motivo de precisar pedir a descrição.
func (h *Handler) readPost(ctx context.Context, userID, chatID int64, raw, canonical, plat string, o outcome) (string, error) {
	if o.video {
		h.Reply(ctx, chatID, fmt.Sprintf("⏳ %s. Vou usar o título e a descrição.", upperFirst(o.reason)))
	} else {
		h.Reply(ctx, chatID, "⏳ Lendo o post…")
	}

	c, err := h.fetchContent(ctx, raw, plat)
	if err != nil {
		slog.Warn("leitura do conteúdo falhou", "url", raw, "err", err)
		switch {
		case errors.Is(err, page.ErrBlocked):
			return "o site bloqueou o acesso ou pede login", nil
		case errors.Is(err, page.ErrNoContent):
			return "a página não tem texto aproveitável", nil
		case errors.Is(err, page.ErrUnavailable):
			return "a leitura de páginas não está disponível", nil
		}
		return "não consegui abrir a página", nil
	}

	actx, cancel := context.WithTimeout(ctx, aiTimeout)
	defer cancel()
	a, err := h.AI.AnalyzePost(actx, ai.Post{URL: raw, Platform: plat, Title: c.Title,
		Author: c.Author, SiteName: c.SiteName, Text: c.Text})
	if err != nil {
		slog.Warn("análise do conteúdo por IA falhou", "url", raw, "err", err)
		return "a IA não conseguiu analisar o conteúdo", nil
	}
	if !a.HasContent {
		return "o que consegui ler não descreve o conteúdo (tela de login, bloqueio ou pouco texto)", nil
	}

	// O título verdadeiro do vídeo vale mais que o gerado; em posts, o da IA é melhor que o da página.
	title, source, kind := shorten(a.Title, maxTitle), "page", "post"
	if c.videoMeta {
		title, source, kind = c.Title, "metadata", "por título e descrição"
	}
	if title == "" {
		title = shorten(c.Title, maxTitle)
	}
	// O texto lido vai na coluna transcript: é o "texto extraído do conteúdo",
	// e já entra no índice full-text.
	it := store.Item{
		UserID: userID, URL: raw, CanonicalURL: canonical, Platform: plat,
		Title: title, Transcript: c.Text, Summary: a.Summary, Tags: a.Tags, Source: source,
	}
	id, err := h.Store.Insert(ctx, &it)
	if err != nil {
		return "", err
	}
	h.embed(ctx, userID, id, embedText(it))

	msg := savedMessage(id, plat, kind, title, it.Summary, it.Tags)
	if c.Partial {
		msg += fmt.Sprintf("\n⚠️ O site só entregou o começo do texto. Use /editar %d para completar.", id)
	}
	h.Reply(ctx, chatID, msg)
	return "", nil
}

// fetchContent lê o texto do link. Em plataformas de vídeo, os metadados do
// yt-dlp (descrição completa, canal, tags) valem mais que a página, que entrega
// só um trecho; se faltarem, lê a página.
func (h *Handler) fetchContent(ctx context.Context, raw, plat string) (content, error) {
	if u, err := url.Parse(raw); err == nil && platform.Extract[plat] && h.Extractor.Supports(u) {
		mctx, cancel := context.WithTimeout(ctx, pageTimeout)
		m, merr := h.Extractor.Metadata(mctx, u)
		cancel()
		if merr != nil {
			slog.Debug("metadados indisponíveis; lendo a página", "url", raw, "err", merr)
		} else if pg, ok := pageFromMeta(m); ok {
			return content{Page: pg, videoMeta: true}, nil
		}
	}

	rctx, cancel := context.WithTimeout(ctx, pageTimeout)
	defer cancel()
	pg, err := h.pages().Read(rctx, raw)
	return content{Page: pg}, err
}

// pageFromMeta monta o texto a analisar com título, descrição, tags e categorias
// do vídeo. ok é falso se sobrar pouco texto para dizer algo.
func pageFromMeta(m extract.Meta) (page.Page, bool) {
	text := m.Description
	if len(m.Tags) > 0 {
		text += "\n\nTags do vídeo: " + strings.Join(m.Tags, ", ")
	}
	if len(m.Categories) > 0 {
		text += "\nCategoria: " + strings.Join(m.Categories, ", ")
	}
	text = strings.TrimSpace(text)
	if r := []rune(text); len(r) > maxMetaText {
		text = string(r[:maxMetaText])
	}
	ok := utf8.RuneCountInString(m.Title)+utf8.RuneCountInString(m.Description) >= minMetaRunes
	return page.Page{Title: m.Title, Author: m.Uploader, Text: text}, ok
}

func upperFirst(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return strings.ToUpper(string(r[:1])) + string(r[1:])
}
