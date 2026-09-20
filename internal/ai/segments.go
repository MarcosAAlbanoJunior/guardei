package ai

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// segmentTimeout vale para cada pedaço de áudio.
const segmentTimeout = 60 * time.Second

// AnalyzeSegments transcreve um áudio já fatiado. Em áudios longos o modelo
// para antes do fim ou entra em repetição, então cada pedaço (~5 min) é
// transcrito à parte e o resumo e as tags saem da transcrição completa.
// HasSpeech é falso se nenhum pedaço tiver fala.
func AnalyzeSegments(ctx context.Context, c AIClient, segments [][]byte, mime string) (Analysis, error) {
	if len(segments) == 0 {
		return Analysis{}, fmt.Errorf("nenhum trecho de áudio")
	}
	if len(segments) == 1 {
		sctx, cancel := context.WithTimeout(ctx, segmentTimeout)
		defer cancel()
		return c.AnalyzeAudio(sctx, segments[0], mime)
	}

	var parts []string
	for i, seg := range segments {
		sctx, cancel := context.WithTimeout(ctx, segmentTimeout)
		a, err := c.AnalyzeAudio(sctx, seg, mime)
		cancel()
		if err != nil {
			return Analysis{}, fmt.Errorf("trecho %d de %d: %w", i+1, len(segments), err)
		}
		if a.HasSpeech {
			parts = append(parts, a.Transcript)
		}
	}
	if len(parts) == 0 {
		return Analysis{}, nil
	}

	full := strings.Join(parts, " ")
	sctx, cancel := context.WithTimeout(ctx, segmentTimeout)
	defer cancel()
	a, err := c.AnalyzeTranscript(sctx, full)
	if err != nil {
		return Analysis{}, fmt.Errorf("resumo da transcrição: %w", err)
	}
	return Analysis{HasSpeech: true, Transcript: full, Summary: a.Summary, Tags: a.Tags}, nil
}
