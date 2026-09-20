//go:build live

package ai

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

// Testes contra a API real (go test -tags live ./internal/ai): exigem GEMINI_API_KEY.
func liveClient(t *testing.T) Client {
	t.Helper()
	key := os.Getenv("GEMINI_API_KEY")
	if key == "" {
		t.Skip("GEMINI_API_KEY não definida")
	}
	model, emb := os.Getenv("GEMINI_MODEL"), os.Getenv("GEMINI_EMBEDDING_MODEL")
	if model == "" {
		model = "gemini-2.5-flash-lite"
	}
	if emb == "" {
		emb = "gemini-embedding-2"
	}
	return New(key, model, emb)
}

func TestLiveText(t *testing.T) {
	c := liveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	a, err := c.AnalyzeText(ctx, "receita de pão de queijo mineiro com polvilho azedo")
	if err != nil || a.Summary == "" || len(a.Tags) == 0 {
		t.Fatalf("%+v %v", a, err)
	}
	t.Logf("resumo=%q tags=%v", a.Summary, a.Tags)
	v, err := c.Embed(ctx, a.Summary, TaskDocument)
	if err != nil || len(v) != EmbeddingDims {
		t.Fatalf("dims=%d err=%v", len(v), err)
	}
}

// Sem fala (um tom puro), o modelo deve marcar has_speech=false.
func TestLiveAudioWithoutSpeech(t *testing.T) {
	c := liveClient(t)
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg ausente")
	}
	out := t.TempDir() + "/tone.mp3"
	if b, err := exec.Command("ffmpeg", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=4",
		"-ac", "1", "-ar", "16000", "-b:a", "32k", out).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v %s", err, b)
	}
	audio, _ := os.ReadFile(out)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	a, err := c.AnalyzeAudio(ctx, audio, "audio/mp3")
	if err != nil {
		t.Fatal(err)
	}
	if a.HasSpeech {
		t.Fatalf("tom puro marcado como fala: %+v", a)
	}
}
