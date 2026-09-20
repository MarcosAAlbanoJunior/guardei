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
	// EmbeddingDims é a dimensão dos vetores: 768 ocupa cerca de 3 KB por item.
	EmbeddingDims = 768

	// Tetos de saída: um pedaço de 5 min de fala gasta ~1,5 mil tokens. O teto
	// folgado não atrapalha e limita o custo se o modelo entrar em repetição
	// (medido: 65 mil tokens numa transcrição de 25 min sem fatiar).
	maxAudioOutputTokens = 8192
	maxTextOutputTokens  = 2048
)

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
	return g.analyzeText(ctx, "Descrição escrita pelo dono do vídeo:\n"+text)
}

func (g *gemini) AnalyzeTranscript(ctx context.Context, transcript string) (Analysis, error) {
	return g.analyzeText(ctx, "Transcrição do áudio do vídeo:\n"+transcript)
}

func (g *gemini) AnalyzePost(ctx context.Context, p Post) (Analysis, error) {
	text := p.Text
	if r := []rune(text); len(r) > maxPostRunes {
		text = string(r[:maxPostRunes])
	}
	var sb strings.Builder
	sb.WriteString(postPrompt + "\n\n")
	for _, f := range []struct{ k, v string }{{"URL", p.URL}, {"Plataforma", p.Platform}, {"Site", p.SiteName}, {"Autor", p.Author}, {"Título da página", p.Title}} {
		if f.v != "" {
			fmt.Fprintf(&sb, "%s: %s\n", f.k, f.v)
		}
	}
	sb.WriteString("\nConteúdo:\n\"\"\"\n" + text + "\n\"\"\"")

	a, err := g.generate(ctx, []any{map[string]any{"text": sb.String()}}, postSchema, maxTextOutputTokens)
	if err != nil {
		return a, err
	}
	if !a.HasContent {
		return Analysis{}, nil
	}
	return a, nil
}

func (g *gemini) analyzeText(ctx context.Context, prompt string) (Analysis, error) {
	a, err := g.generate(ctx, []any{map[string]any{"text": prompt}}, textSchema, maxTextOutputTokens)
	if err != nil {
		return a, err
	}
	// Texto sempre "tem conteúdo"; o campo só faz sentido para áudio.
	a.HasSpeech = true
	return a, nil
}

func (g *gemini) AnalyzeAudio(ctx context.Context, audio []byte, mime string) (Analysis, error) {
	parts := []any{
		map[string]any{"inlineData": map[string]any{"mimeType": mime, "data": base64.StdEncoding.EncodeToString(audio)}},
		map[string]any{"text": audioPrompt},
	}
	a, err := g.generate(ctx, parts, analysisSchema, maxAudioOutputTokens)
	if err != nil {
		return a, err
	}
	// Salvaguarda contra alucinação em áudio sem fala: não confia só no has_speech.
	if !a.HasSpeech || spokenWords(a.Transcript) < minSpokenWords {
		return Analysis{}, nil
	}
	return a, nil
}

func (g *gemini) generate(ctx context.Context, parts []any, schema map[string]any, maxOutputTokens int) (Analysis, error) {
	body := map[string]any{
		"systemInstruction": map[string]any{"parts": []any{map[string]any{"text": systemPrompt}}},
		"contents":          []any{map[string]any{"role": "user", "parts": parts}},
		"generationConfig": map[string]any{
			"temperature":      0.2,
			"maxOutputTokens":  maxOutputTokens,
			"responseMimeType": "application/json",
			"responseSchema":   schema,
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
	// Só STOP é resposta completa. MAX_TOKENS (transcrição cortada ou em laço de
	// repetição) e os demais motivos viram erro: melhor pedir descrição do que
	// salvar uma transcrição pela metade.
	if fr := resp.Candidates[0].FinishReason; fr != "" && fr != "STOP" {
		return Analysis{}, fmt.Errorf("resposta incompleta do gemini (%s)", fr)
	}
	var sb strings.Builder
	for _, p := range resp.Candidates[0].Content.Parts {
		sb.WriteString(p.Text)
	}
	var a Analysis
	if err := json.Unmarshal([]byte(sb.String()), &a); err != nil {
		return Analysis{}, fmt.Errorf("gemini devolveu JSON inválido (%s): %w", resp.Candidates[0].FinishReason, err)
	}
	a.Title = strings.TrimSpace(a.Title)
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
