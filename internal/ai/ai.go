// Package ai isola o provedor de IA atrás de uma interface. Sem chave, a
// implementação é nopAI e o bot cai no modo manual.
package ai

import (
	"context"
	"errors"
)

// ErrAIDisabled é o erro de toda chamada quando não há GEMINI_API_KEY.
var ErrAIDisabled = errors.New("IA desligada")

// Analysis é o resultado da análise de um vídeo (áudio) ou de uma descrição.
type Analysis struct {
	HasSpeech  bool     `json:"has_speech"`  // só análise de áudio
	HasContent bool     `json:"has_content"` // só análise de post
	Transcript string   `json:"transcript"`
	Title      string   `json:"title"` // só análise de post
	Summary    string   `json:"summary"`
	Tags       []string `json:"tags"`
}

// Post é o que foi lido de um post ou página pública.
type Post struct {
	URL, Platform, Title, Author, SiteName, Text string
}

// EmbedTask diferencia o lado do documento e o da consulta na busca.
type EmbedTask int

// Tarefas de embedding.
const (
	TaskDocument EmbedTask = iota
	TaskQuery
)

// Client é o provedor de IA usado pelo bot. Sem chave, a implementação é uma
// versão que só devolve ErrAIDisabled e o bot cai no modo manual.
type Client interface {
	Enabled() bool
	AnalyzeAudio(ctx context.Context, audio []byte, mime string) (Analysis, error)
	AnalyzeText(ctx context.Context, text string) (Analysis, error)
	// AnalyzePost analisa o texto de um post ou página. HasContent é falso quando
	// o texto é tela de login, erro ou aviso, e não descreve o post.
	AnalyzePost(ctx context.Context, p Post) (Analysis, error)
	// AnalyzeTranscript resume e etiqueta uma transcrição já pronta (Summary e Tags).
	AnalyzeTranscript(ctx context.Context, transcript string) (Analysis, error)
	Embed(ctx context.Context, text string, task EmbedTask) ([]float32, error)
}

// New devolve o cliente Gemini quando há chave, senão o nopAI.
func New(apiKey, model, embeddingModel string) Client {
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
func (nopAI) AnalyzePost(context.Context, Post) (Analysis, error)   { return Analysis{}, ErrAIDisabled }
func (nopAI) AnalyzeTranscript(context.Context, string) (Analysis, error) {
	return Analysis{}, ErrAIDisabled
}
func (nopAI) Embed(context.Context, string, EmbedTask) ([]float32, error) {
	return nil, ErrAIDisabled
}
