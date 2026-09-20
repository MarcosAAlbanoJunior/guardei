// Package extract baixa o áudio de um vídeo e o converte para um formato que o Gemini aceita.
package extract

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"
)

// AudioFile é o áudio pronto para a IA: mp3 mono, com duração e tamanho já
// validados, fatiado em pedaços de até ~5 min (um só, se o vídeo for curto).
type AudioFile struct {
	Segments [][]byte
	Mime     string
	Duration time.Duration
	Title    string
}

// Meta são os metadados públicos de um vídeo: o que resta quando não há áudio
// aproveitável (sem fala, longo demais, ao vivo).
type Meta struct {
	Title       string
	Description string
	Uploader    string
	Tags        []string
	Categories  []string
	Duration    time.Duration // zero se desconhecida (ao vivo)
}

// Extractor baixa o áudio de vídeos de uma plataforma.
type Extractor interface {
	// Supports diz se este extrator sabe tratar o link.
	Supports(u *url.URL) bool
	// Audio baixa e prepara o áudio. started (pode ser nil) é chamado quando o link
	// tem vídeo dentro do limite e o download vai começar, para o bot avisar o
	// usuário só então: um post de texto no X nunca chega aqui.
	Audio(ctx context.Context, u *url.URL, started func()) (AudioFile, error)
	// Metadata lê título, descrição, canal e tags do vídeo, sem baixar nada.
	Metadata(ctx context.Context, u *url.URL) (Meta, error)
}

var (
	// ErrNoDuration indica transmissão ao vivo ou vídeo sem duração conhecida.
	ErrNoDuration = errors.New("vídeo sem duração conhecida (ao vivo?)")
	// ErrTooLarge indica áudio acima do limite de tamanho da requisição à IA.
	ErrTooLarge = errors.New("áudio grande demais")
)

// TooLongError indica um vídeo mais longo que MAX_VIDEO_SECONDS.
type TooLongError struct{ Duration, Max time.Duration }

func (e *TooLongError) Error() string {
	return fmt.Sprintf("vídeo com %s, acima do limite de %s", e.Duration.Round(time.Second), e.Max.Round(time.Second))
}

// Nop é o extrator usado quando yt-dlp ou ffmpeg não estão disponíveis.
type Nop struct{}

// Supports sempre devolve falso: não há extrator.
func (Nop) Supports(*url.URL) bool { return false }

// Metadata sempre falha: não há extrator.
func (Nop) Metadata(context.Context, *url.URL) (Meta, error) {
	return Meta{}, errors.New("extração indisponível")
}

// Audio sempre falha: não há extrator.
func (Nop) Audio(context.Context, *url.URL, func()) (AudioFile, error) {
	return AudioFile{}, errors.New("extração indisponível")
}
