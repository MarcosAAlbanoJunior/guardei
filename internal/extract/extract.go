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

type Extractor interface {
	Supports(u *url.URL) bool
	// Audio baixa e prepara o áudio. started (pode ser nil) é chamado quando o link
	// tem vídeo dentro do limite e o download vai começar, para o bot avisar o
	// usuário só então: um post de texto no X nunca chega aqui.
	Audio(ctx context.Context, u *url.URL, started func()) (AudioFile, error)
}

var (
	// ErrNoDuration: transmissão ao vivo ou vídeo sem duração conhecida.
	ErrNoDuration = errors.New("vídeo sem duração conhecida (ao vivo?)")
	ErrTooLarge   = errors.New("áudio grande demais")
)

// TooLongError: o vídeo passa de MAX_VIDEO_SECONDS.
type TooLongError struct{ Duration, Max time.Duration }

func (e *TooLongError) Error() string {
	return fmt.Sprintf("vídeo com %s, acima do limite de %s", e.Duration.Round(time.Second), e.Max.Round(time.Second))
}

// Nop é o extrator usado quando yt-dlp ou ffmpeg não estão disponíveis.
type Nop struct{}

func (Nop) Supports(*url.URL) bool { return false }
func (Nop) Audio(context.Context, *url.URL, func()) (AudioFile, error) {
	return AudioFile{}, errors.New("extração indisponível")
}
