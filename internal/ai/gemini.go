package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"
)

const (
	defaultBaseURL = "https://generativelanguage.googleapis.com/v1beta"
	// EmbeddingDims: 768 mantém cerca de 3 KB por item.
	EmbeddingDims = 768
)

const systemPrompt = `Você organiza uma biblioteca pessoal de vídeos salvos, para o dono achá-los depois por busca.
Responda sempre em português do Brasil. Não invente nada que não esteja no conteúdo recebido.
- summary: 1 a 2 frases dizendo do que o vídeo trata.
- tags: de 3 a 6 palavras-chave curtas, em minúsculas, úteis para busca.`

const audioPrompt = `Analise o áudio de um vídeo curto e diga se ele contém fala humana.
- has_speech: true somente se você ouvir pessoas falando palavras compreensíveis. Música, tons, ruídos, sons ambiente e silêncio NÃO são fala: nesses casos, has_speech=false.
- transcript: apenas as palavras faladas, sem timestamps, sem descrever sons. Se não há fala, "".
- summary e tags: baseados só no que foi dito. Se não há fala, summary "" e tags [].
Nunca descreva ou invente conteúdo que não foi falado.`

var analysisSchema = map[string]any{
	"type": "OBJECT",
	"properties": map[string]any{
		"has_speech": map[string]any{"type": "BOOLEAN"},
		"transcript": map[string]any{"type": "STRING"},
		"summary":    map[string]any{"type": "STRING"},
		"tags":       map[string]any{"type": "ARRAY", "items": map[string]any{"type": "STRING"}},
	},
	"required": []string{"has_speech", "summary", "tags"},
}

type gemini struct {
	key, model, embModel string
	baseURL              string
	http                 *http.Client
}

func newGemini(key, model, embModel string) *gemini {
	return &gemini{key: key, model: model, embModel: embModel, baseURL: defaultBaseURL,
		http: &http.Client{Timeout: 90 * time.Second}}
}

func (g *gemini) Enabled() bool { return true }

func (g *gemini) AnalyzeText(ctx context.Context, text string) (Analysis, error) {
	parts := []any{map[string]any{"text": "Descrição escrita pelo dono do vídeo:\n" + text}}
	a, err := g.generate(ctx, parts)
	if err != nil {
		return a, err
	}
	// Uma descrição sempre "tem conteúdo"; o campo só faz sentido para áudio.
	a.HasSpeech = true
	return a, nil
}

func (g *gemini) AnalyzeAudio(ctx context.Context, audio []byte, mime string) (Analysis, error) {
	parts := []any{
		map[string]any{"inlineData": map[string]any{"mimeType": mime, "data": base64.StdEncoding.EncodeToString(audio)}},
		map[string]any{"text": audioPrompt},
	}
	a, err := g.generate(ctx, parts)
	if err != nil {
		return a, err
	}
	// Salvaguarda contra alucinação em áudio sem fala: não confia só no has_speech.
	if a.HasSpeech && spokenWords(a.Transcript) < minSpokenWords {
		a = Analysis{}
	} else if !a.HasSpeech {
		a = Analysis{}
	}
	return a, nil
}

func (g *gemini) generate(ctx context.Context, parts []any) (Analysis, error) {
	body := map[string]any{
		"systemInstruction": map[string]any{"parts": []any{map[string]any{"text": systemPrompt}}},
		"contents":          []any{map[string]any{"role": "user", "parts": parts}},
		"generationConfig": map[string]any{
			"temperature":      0.2,
			"responseMimeType": "application/json",
			"responseSchema":   analysisSchema,
		},
	}
	var resp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		PromptFeedback struct {
			BlockReason string `json:"blockReason"`
		} `json:"promptFeedback"`
	}
	if err := g.post(ctx, "models/"+g.model+":generateContent", body, &resp); err != nil {
		return Analysis{}, err
	}
	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return Analysis{}, fmt.Errorf("gemini sem resposta (bloqueio: %q)", resp.PromptFeedback.BlockReason)
	}
	var sb strings.Builder
	for _, p := range resp.Candidates[0].Content.Parts {
		sb.WriteString(p.Text)
	}
	var a Analysis
	if err := json.Unmarshal([]byte(sb.String()), &a); err != nil {
		return Analysis{}, fmt.Errorf("gemini devolveu JSON inválido (%s): %w", resp.Candidates[0].FinishReason, err)
	}
	a.Summary = strings.TrimSpace(a.Summary)
	a.Transcript = strings.TrimSpace(a.Transcript)
	a.Tags = cleanTags(a.Tags)
	return a, nil
}

// minSpokenWords: abaixo disso a "transcrição" é lixo (timestamps, sons descritos).
const minSpokenWords = 3

// spokenWords conta palavras de verdade: ignora tokens sem letras (como "00:01").
func spokenWords(s string) int {
	n := 0
	for _, w := range strings.Fields(s) {
		if strings.IndexFunc(w, unicode.IsLetter) >= 0 {
			n++
		}
	}
	return n
}

func (g *gemini) Embed(ctx context.Context, text string, task EmbedTask) ([]float32, error) {
	// O gemini-embedding-2 não usa task_type: a tarefa vai no próprio texto.
	if task == TaskQuery {
		text = "task: search result | query: " + text
	} else {
		text = "title: none | text: " + text
	}
	body := map[string]any{
		"content":               map[string]any{"parts": []any{map[string]any{"text": text}}},
		"output_dimensionality": EmbeddingDims,
	}
	var resp struct {
		Embedding struct {
			Values []float32 `json:"values"`
		} `json:"embedding"`
	}
	if err := g.post(ctx, "models/"+g.embModel+":embedContent", body, &resp); err != nil {
		return nil, err
	}
	if len(resp.Embedding.Values) == 0 {
		return nil, errors.New("gemini devolveu embedding vazio")
	}
	return resp.Embedding.Values, nil
}

// post envia a requisição, com uma nova tentativa em 429 e 5xx.
func (g *gemini) post(ctx context.Context, path string, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(2 * time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+"/"+path, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-goog-api-key", g.key) // no header, nunca na URL: não vaza em logs
		res, err := g.http.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		data, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
		res.Body.Close()
		if res.StatusCode == http.StatusOK {
			return json.Unmarshal(data, out)
		}
		lastErr = fmt.Errorf("gemini HTTP %d: %s", res.StatusCode, apiMessage(data))
		if res.StatusCode != http.StatusTooManyRequests && res.StatusCode < 500 {
			return lastErr
		}
	}
	return lastErr
}

func apiMessage(data []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &e) == nil && e.Error.Message != "" {
		return e.Error.Message
	}
	if len(data) > 200 {
		data = data[:200]
	}
	return string(data)
}

func cleanTags(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(t, "#")))
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}
