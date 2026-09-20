package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fakeServer(t *testing.T, handler http.HandlerFunc) *gemini {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	g := newGemini("k", "m1", "e1")
	g.baseURL = srv.URL
	return g
}

func TestAnalyzeText(t *testing.T) {
	g := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "k" || r.URL.Query().Get("key") != "" {
			t.Error("chave deve ir só no header")
		}
		if !strings.HasSuffix(r.URL.Path, "/models/m1:generateContent") {
			t.Errorf("path %s", r.URL.Path)
		}
		io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"{\"has_speech\":true,\"summary\":\" Pão de queijo. \",\"tags\":[\"Receita\",\"#receita\",\"pão\"]}"}]}}]}`)
	})
	a, err := g.AnalyzeText(context.Background(), "pão de queijo")
	if err != nil {
		t.Fatal(err)
	}
	if a.Summary != "Pão de queijo." || len(a.Tags) != 2 || a.Tags[0] != "receita" || !a.HasSpeech {
		t.Fatalf("%+v", a)
	}
}

func TestAnalyzeAudioSendsInlineData(t *testing.T) {
	g := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if !strings.Contains(mustJSON(body), `"mimeType":"audio/mp3"`) {
			t.Errorf("sem inlineData: %v", body)
		}
		io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"{\"has_speech\":false,\"summary\":\"\",\"tags\":[]}"}]}}]}`)
	})
	a, err := g.AnalyzeAudio(context.Background(), []byte("abc"), "audio/mp3")
	if err != nil || a.HasSpeech {
		t.Fatalf("%+v %v", a, err)
	}
}

func TestEmbedUsesTaskPrefixAndDims(t *testing.T) {
	var got string
	g := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		io.WriteString(w, `{"embedding":{"values":[0.1,0.2]}}`)
	})
	v, err := g.Embed(context.Background(), "queijo", TaskQuery)
	if err != nil || len(v) != 2 {
		t.Fatal(v, err)
	}
	if !strings.Contains(got, "task: search result | query: queijo") || !strings.Contains(got, `"output_dimensionality":768`) {
		t.Errorf("corpo: %s", got)
	}
}

func TestRetryOn429ButNotOn400(t *testing.T) {
	var n atomic.Int32
	g := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		io.WriteString(w, `{"embedding":{"values":[1]}}`)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := g.Embed(ctx, "x", TaskDocument); err != nil || n.Load() != 2 {
		t.Fatalf("err=%v tentativas=%d", err, n.Load())
	}

	n.Store(0)
	g2 := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"message":"modelo inválido"}}`)
	})
	_, err := g2.Embed(ctx, "x", TaskDocument)
	if err == nil || !strings.Contains(err.Error(), "modelo inválido") || n.Load() != 1 {
		t.Fatalf("err=%v tentativas=%d", err, n.Load())
	}
}

func TestNopAI(t *testing.T) {
	c := New("", "", "")
	if c.Enabled() {
		t.Fatal("deveria estar desligada")
	}
	if _, err := c.AnalyzeText(context.Background(), "x"); err != ErrAIDisabled {
		t.Fatal(err)
	}
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestAnalyzeAudioDiscardsFakeSpeech(t *testing.T) {
	// has_speech=true mas a "transcrição" são só timestamps: descartado.
	g := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"{\"has_speech\":true,\"transcript\":\"00:01 00:02\",\"summary\":\"chuva\",\"tags\":[\"chuva\"]}"}]}}]}`)
	})
	a, err := g.AnalyzeAudio(context.Background(), []byte("x"), "audio/mp3")
	if err != nil || a.HasSpeech || a.Summary != "" || len(a.Tags) != 0 {
		t.Fatalf("%+v %v", a, err)
	}
}

func TestSpokenWords(t *testing.T) {
	if spokenWords("00:01 00:02") != 0 || spokenWords("olá, tudo bem?") != 3 {
		t.Fatal("spokenWords")
	}
}

// Regressão: com "transcript" fora de required, o gemini-2.5-flash-lite devolvia
// a transcrição vazia em vídeos com fala, e o vídeo era tratado como "sem fala".
func TestSchemaRequiresTranscript(t *testing.T) {
	req, _ := analysisSchema["required"].([]string)
	for _, f := range []string{"has_speech", "transcript", "summary", "tags"} {
		found := false
		for _, r := range req {
			found = found || r == f
		}
		if !found {
			t.Errorf("%s deveria ser obrigatório no schema", f)
		}
	}
}
