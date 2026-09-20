// Package ai isola o provedor de IA atrás de uma interface. Sem chave, a
// implementação é nopAI e o bot cai no modo manual.
package ai

import (
	"context"
	"errors"
)

var ErrAIDisabled = errors.New("IA desligada")

// Analysis é o resultado da análise de um vídeo (áudio) ou de uma descrição.
type Analysis struct {
	HasSpeech  bool     `json:"has_speech"`
	Transcript string   `json:"transcript"`
	Summary    string   `json:"summary"`
	Tags       []string `json:"tags"`
}

// EmbedTask diferencia o lado do documento e o da consulta na busca.
type EmbedTask int

const (
	TaskDocument EmbedTask = iota
	TaskQuery
)

type AIClient interface {
	Enabled() bool
	AnalyzeAudio(ctx context.Context, audio []byte, mime string) (Analysis, error)
	AnalyzeText(ctx context.Context, text string) (Analysis, error)
	// AnalyzeTranscript resume e etiqueta uma transcrição já pronta (Summary e Tags).
	AnalyzeTranscript(ctx context.Context, transcript string) (Analysis, error)
	Embed(ctx context.Context, text string, task EmbedTask) ([]float32, error)
}

// New devolve o cliente Gemini quando há chave, senão o nopAI.
func New(apiKey, model, embeddingModel string) AIClient {
	if apiKey == "" {
		return nopAI{}
	}
	return newGemini(apiKey, model, embeddingModel)
}

type nopAI struct{}

func (nopAI) Enabled() bool { return false }
func (nopAI) AnalyzeAudio(context.Context, []byte, string) (Analysis, error) {
	return Analysis{}, ErrAIDisabled
}
func (nopAI) AnalyzeText(context.Context, string) (Analysis, error) { return Analysis{}, ErrAIDisabled }
func (nopAI) AnalyzeTranscript(context.Context, string) (Analysis, error) {
	return Analysis{}, ErrAIDisabled
}
func (nopAI) Embed(context.Context, string, EmbedTask) ([]float32, error) {
	return nil, ErrAIDisabled
}
