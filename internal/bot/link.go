package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/MarcosAAlbanoJunior/guardei/internal/ai"
	"github.com/MarcosAAlbanoJunior/guardei/internal/extract"
	"github.com/MarcosAAlbanoJunior/guardei/internal/platform"
	"github.com/MarcosAAlbanoJunior/guardei/internal/store"
)

// link trata uma mensagem com link. Com texto junto, salva na hora; sem texto,
// tenta analisar o link sozinho e, se não der, pede uma descrição.
func (h *Handler) link(ctx context.Context, userID, chatID int64, raw, note string) error {
	plat, canonical, err := platform.Canonical(raw)
	if err != nil {
		h.Reply(ctx, chatID, "Não consegui entender esse link.")
		return nil
	}
	// Novo link encerra qualquer espera anterior.
	if _, err := h.Store.ClearPending(ctx, chatID); err != nil {
		return err
	}

	existing, err := h.Store.FindByCanonical(ctx, userID, canonical)
	switch {
	case err == nil:
		return h.alreadySaved(ctx, userID, chatID, raw, note, existing)
	case !errors.Is(err, store.ErrNotFound):
		return err
	}

	if note != "" {
		return h.save(ctx, userID, chatID, raw, canonical, plat, note)
	}
	reason, err := h.auto(ctx, userID, chatID, raw, canonical, plat)
	if err != nil || reason == "" {
		return err // erro, ou salvo automaticamente
	}
	if err := h.Store.SetPending(ctx, store.Pending{ChatID: chatID, URL: raw, Reason: reason}); err != nil {
		return err
	}
	h.Reply(ctx, chatID, fmt.Sprintf("Não consegui analisar esse link: %s.\nMe diga do que ele trata e eu guardo (ou /cancelar).", reason))
	return nil
}

// alreadySaved trata um link repetido: atualiza a descrição se veio texto, senão oferece atualizar.
func (h *Handler) alreadySaved(ctx context.Context, userID, chatID int64, raw, note string, existing store.Item) error {
	if note != "" {
		return h.applyNote(ctx, userID, chatID, existing.ID, note)
	}
	if err := h.Store.SetPending(ctx, store.Pending{ChatID: chatID, URL: raw, ItemID: existing.ID}); err != nil {
		return err
	}
	h.Reply(ctx, chatID, fmt.Sprintf("Esse link já está salvo (#%d). Envie uma nova descrição para atualizar, ou /cancelar.", existing.ID))
	return nil
}

// outcome é o resultado de uma tentativa automática. reason vazio = salvou.
type outcome struct {
	reason   string
	readPage bool // vale tentar ler o texto do link como alternativa
	video    bool // o vídeo existe, mas o áudio não serviu: sobram título e descrição
}

// auto salva o link sem ajuda do usuário: primeiro pela transcrição do áudio
// (vídeos) e, se não der, pelo texto do link (título e descrição do vídeo, ou o
// texto do post). Devolve "" quando salvou, ou o motivo de precisar pedir a descrição.
func (h *Handler) auto(ctx context.Context, userID, chatID int64, raw, canonical, plat string) (string, error) {
	o, err := h.transcribe(ctx, userID, chatID, raw, canonical, plat)
	if err != nil || o.reason == "" || !o.readPage {
		return o.reason, err
	}
	reason, err := h.readPost(ctx, userID, chatID, raw, canonical, plat, o)
	if err != nil || reason == "" {
		return "", err
	}
	if platform.Extract[plat] { // era um vídeo: conta as duas falhas
		reason = o.reason + "; " + reason
	}
	return reason, nil
}

// transcribe tenta salvar o vídeo pela transcrição do áudio.
func (h *Handler) transcribe(ctx context.Context, userID, chatID int64, raw, canonical, plat string) (outcome, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return outcome{}, err
	}
	if reason := h.noExtractionReason(plat, u); reason != "" {
		return outcome{reason: reason, readPage: h.AI.Enabled()}, nil
	}

	// O aviso sai só quando o extrator confirma que há vídeo para baixar: um post
	// de texto no X passa por aqui e não deve ver "transcrevendo…".
	af, err := h.Extractor.Audio(ctx, u, func() {
		h.Reply(ctx, chatID, "⏳ Baixando o áudio e transcrevendo…")
	})
	if err != nil {
		slog.Warn("extração de áudio falhou", "url", raw, "err", err)
		o := extractionFailure(err)
		// YouTube e TikTok só têm vídeos; o X também tem posts de texto, que não têm vídeo para baixar.
		o.video = o.video || plat == platform.YouTube || plat == platform.TikTok
		return o, nil
	}

	a, err := ai.AnalyzeSegments(ctx, h.AI, af.Segments, af.Mime)
	if err != nil {
		slog.Warn("transcrição por IA falhou", "url", raw, "err", err)
		return outcome{reason: "a IA não conseguiu transcrever", readPage: true, video: true}, nil
	}
	if !a.HasSpeech {
		return outcome{reason: "o vídeo não tem fala (só música ou ruído)", readPage: true, video: true}, nil
	}

	it := store.Item{
		UserID: userID, URL: raw, CanonicalURL: canonical, Platform: plat,
		Title: af.Title, Transcript: a.Transcript, Summary: a.Summary, Tags: a.Tags, Source: "transcript",
	}
	id, err := h.Store.Insert(ctx, &it)
	if err != nil {
		return outcome{}, err
	}
	h.embed(ctx, userID, id, embedText(it))
	h.Reply(ctx, chatID, savedMessage(id, plat, "transcrito", it.Title, it.Summary, it.Tags))
	return outcome{}, nil
}

// extractionFailure traduz o erro do extrator no motivo mostrado ao usuário. Em
// todos os casos ainda há texto para ler: título e descrição do vídeo, ou o post.
func extractionFailure(err error) outcome {
	var tooLong *extract.TooLongError
	switch {
	case errors.As(err, &tooLong):
		return outcome{reason: fmt.Sprintf("o vídeo é longo demais (%s; o máximo é %s)", fmtDuration(tooLong.Duration), fmtDuration(tooLong.Max)), readPage: true, video: true}
	case errors.Is(err, extract.ErrNoDuration):
		return outcome{reason: "não consegui saber a duração (transmissão ao vivo?)", readPage: true, video: true}
	case errors.Is(err, extract.ErrTooLarge):
		return outcome{reason: "o áudio ficou grande demais", readPage: true, video: true}
	}
	// Sem vídeo para baixar (ex.: post de texto no X) ou bloqueio: o texto do post ainda pode servir.
	return outcome{reason: "não consegui baixar o áudio", readPage: true}
}

func fmtDuration(d time.Duration) string {
	d = d.Round(time.Second)
	return fmt.Sprintf("%d min %02d s", int(d.Minutes()), int(d.Seconds())%60)
}

// noExtractionReason diz por que o áudio nem chega a ser extraído ("" se pode tentar).
func (h *Handler) noExtractionReason(plat string, u *url.URL) string {
	switch {
	case !platform.Extract[plat]:
		return "esta plataforma não tem extração de áudio"
	case !h.AI.Enabled():
		return "a IA não está ligada (sem GEMINI_API_KEY)"
	case !h.Extractor.Supports(u):
		return "o extrator de áudio (yt-dlp e ffmpeg) não está disponível"
	}
	return ""
}

// describe trata a resposta do usuário a um pedido de descrição.
func (h *Handler) describe(ctx context.Context, userID, chatID int64, p store.Pending, text string) error {
	if _, err := h.Store.ClearPending(ctx, chatID); err != nil {
		return err
	}
	if p.ItemID != 0 {
		return h.applyNote(ctx, userID, chatID, p.ItemID, text)
	}
	plat, canonical, err := platform.Canonical(p.URL)
	if err != nil {
		return err
	}
	return h.save(ctx, userID, chatID, p.URL, canonical, plat, text)
}

// save guarda um link com a descrição do usuário, com resumo e tags quando há IA.
func (h *Handler) save(ctx context.Context, userID, chatID int64, raw, canonical, plat, note string) error {
	summary, tags := h.analyze(ctx, note)
	it := store.Item{
		UserID: userID, URL: raw, CanonicalURL: canonical, Platform: plat,
		UserNote: note, Summary: summary, Tags: tags, Source: "manual",
	}
	id, err := h.Store.Insert(ctx, &it)
	if err != nil {
		return err
	}
	h.embed(ctx, userID, id, embedText(it))

	body := note
	if summary != "" {
		body = summary
	}
	h.Reply(ctx, chatID, savedMessage(id, plat, "", "", body, tags))
	return nil
}
