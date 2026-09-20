package extract

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeRun imita o yt-dlp: --print devolve metadados; o download grava o mp3 no -o.
func fakeRun(meta string, audio []byte, downloads *[]string) runner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "ffmpeg" && !slices.Contains(args, "segment") { // conversão: grava o mp3 final
			return nil, os.WriteFile(args[len(args)-1], audio, 0o600)
		}
		if name == "ffmpeg" { // fatia em 3 trechos, como o -f segment faria
			dir := filepath.Dir(args[len(args)-1])
			for i := 0; i < 3; i++ {
				if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("seg_%03d.mp3", i)), []byte{byte('a' + i)}, 0o600); err != nil {
					return nil, err
				}
			}
			return nil, nil
		}
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "--print") {
			return []byte("aviso qualquer\n" + meta + "\n"), nil
		}
		for i, a := range args {
			if a == "-o" {
				dir := filepath.Dir(args[i+1])
				*downloads = append(*downloads, dir)
				// o yt-dlp grava no formato nativo (aqui m4a), não em mp3
				return nil, os.WriteFile(filepath.Join(dir, "raw.m4a"), []byte("cru"), 0o600)
			}
		}
		return nil, errors.New("sem -o")
	}
}

func newFake(meta string, audio []byte, dl *[]string) *YtDlp {
	y := NewYtDlp("yt-dlp", 600*time.Second)
	y.run = fakeRun(meta, audio, dl)
	return y
}

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestAudioOKCleansTempDir(t *testing.T) {
	var dl []string
	y := newFake(`{"duration": 19.0, "title": " Me at the zoo ", "is_live": false}`, []byte("mp3"), &dl)
	af, err := y.Audio(context.Background(), mustURL(t, "https://youtu.be/x"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if af.Title != "Me at the zoo" || af.Mime != "audio/mp3" || af.Duration != 19*time.Second ||
		len(af.Segments) != 1 || string(af.Segments[0]) != "mp3" {
		t.Fatalf("%+v", af)
	}
	if _, err := os.Stat(dl[0]); !os.IsNotExist(err) {
		t.Fatalf("diretório temporário não foi apagado: %v", err)
	}
}

func TestAudioTooLongSkipsDownload(t *testing.T) {
	var dl []string
	y := newFake(`{"duration": 3600, "title": "x", "is_live": false}`, nil, &dl)
	_, err := y.Audio(context.Background(), mustURL(t, "https://youtu.be/x"), nil)
	var tl *TooLongError
	if !errors.As(err, &tl) || len(dl) != 0 {
		t.Fatalf("err=%v downloads=%v", err, dl)
	}
}

func TestAudioLiveOrNoDuration(t *testing.T) {
	var dl []string
	for _, meta := range []string{`{"duration": null, "title": "x", "is_live": true}`, `{"duration": null, "title": "x", "is_live": false}`} {
		y := newFake(meta, nil, &dl)
		if _, err := y.Audio(context.Background(), mustURL(t, "https://youtu.be/x"), nil); !errors.Is(err, ErrNoDuration) {
			t.Fatalf("%s: %v", meta, err)
		}
	}
}

func TestAudioTooLargeAndCleanup(t *testing.T) {
	var dl []string
	y := newFake(`{"duration": 60, "title": "x", "is_live": false}`, make([]byte, maxAudioBytes+1), &dl)
	_, err := y.Audio(context.Background(), mustURL(t, "https://youtu.be/x"), nil)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	if _, err := os.Stat(dl[0]); !os.IsNotExist(err) {
		t.Fatal("diretório temporário não foi apagado após erro")
	}
}

func TestURLGoesAfterDoubleDash(t *testing.T) {
	var seen []string
	y := NewYtDlp("yt-dlp", time.Minute)
	y.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		seen = args
		return nil, errors.New("para aqui")
	}
	y.Audio(context.Background(), mustURL(t, "https://youtu.be/--exec=evil"), nil)
	if len(seen) < 2 || seen[len(seen)-2] != "--" {
		t.Fatalf("URL deve vir depois de --: %v", seen)
	}
}

func TestSupports(t *testing.T) {
	y := NewYtDlp("yt-dlp", time.Minute)
	for raw, want := range map[string]bool{
		"https://youtu.be/x":                true,
		"https://www.instagram.com/reel/x/": false,
		"https://example.com/video":         false,
	} {
		if got := y.Supports(mustURL(t, raw)); got != want {
			t.Errorf("%s: %v", raw, got)
		}
	}
}

func TestLongAudioIsSplitIntoSegments(t *testing.T) {
	var dl []string
	y := newFake(`{"duration": 590, "title": "x", "is_live": false}`, []byte("inteiro"), &dl)
	af, err := y.Audio(context.Background(), mustURL(t, "https://youtu.be/x"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(af.Segments) != 3 || string(af.Segments[0]) != "a" || string(af.Segments[2]) != "c" {
		t.Fatalf("segmentos fora de ordem ou faltando: %q", af.Segments)
	}
	if _, err := os.Stat(dl[0]); !os.IsNotExist(err) {
		t.Fatal("diretório temporário não foi apagado")
	}
}

func TestSplitFailureIsAnError(t *testing.T) {
	var dl []string
	y := newFake(`{"duration": 590, "title": "x", "is_live": false}`, []byte("x"), &dl)
	base := y.run
	y.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "ffmpeg" {
			return []byte("Invalid data"), errors.New("exit 1")
		}
		return base(ctx, name, args...)
	}
	if _, err := y.Audio(context.Background(), mustURL(t, "https://youtu.be/x"), nil); err == nil || !strings.Contains(err.Error(), "ffmpeg") {
		t.Fatal(err)
	}
}

// Regressão: o yt-dlp não converte áudio que já vem em mp3 (TikTok); a
// conversão para mono/16 kHz/32 kbps precisa ser feita sempre.
func TestAlwaysConvertsWithFFmpeg(t *testing.T) {
	var dl []string
	y := newFake(`{"duration": 60, "title": "x", "is_live": false}`, []byte("convertido"), &dl)
	var ffmpegArgs []string
	base := y.run
	y.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "ffmpeg" {
			ffmpegArgs = args
		}
		return base(ctx, name, args...)
	}
	af, err := y.Audio(context.Background(), mustURL(t, "https://youtu.be/x"), nil)
	if err != nil || string(af.Segments[0]) != "convertido" {
		t.Fatalf("%v %q", err, af.Segments)
	}
	joined := strings.Join(ffmpegArgs, " ")
	for _, want := range []string{"-ac 1", "-ar 16000", "-b:a 32k", "-vn"} {
		if !strings.Contains(joined, want) {
			t.Errorf("ffmpeg sem %q: %s", want, joined)
		}
	}
}

// O aviso "baixando…" só pode sair quando há vídeo dentro do limite.
func TestStartedOnlyWhenThereIsAVideoToDownload(t *testing.T) {
	var dl []string
	count := func(meta string, probeErr error) int {
		y := newFake(meta, []byte("mp3"), &dl)
		if probeErr != nil {
			y.run = func(context.Context, string, ...string) ([]byte, error) {
				return []byte("ERROR: No video could be found in this tweet"), probeErr
			}
		}
		n := 0
		y.Audio(context.Background(), mustURL(t, "https://x.com/u/status/1"), func() { n++ })
		return n
	}
	if n := count(`{"duration": 30, "title": "x", "is_live": false}`, nil); n != 1 {
		t.Errorf("vídeo válido: started=%d", n)
	}
	if n := count("", errors.New("exit 1")); n != 0 { // tweet sem vídeo: a sondagem falha
		t.Errorf("sem vídeo: started=%d", n)
	}
	if n := count(`{"duration": 3600, "title": "x", "is_live": false}`, nil); n != 0 {
		t.Errorf("vídeo longo: started=%d", n)
	}
	if n := count(`{"duration": null, "title": "x", "is_live": true}`, nil); n != 0 {
		t.Errorf("ao vivo: started=%d", n)
	}
}

func TestAvailable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	if err := Available("yt-dlp"); err == nil || !strings.Contains(err.Error(), "yt-dlp") {
		t.Fatalf("sem binários: %v", err)
	}
	for _, name := range []string{"yt-dlp", "ffmpeg"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := Available("yt-dlp"); err != nil {
		t.Fatalf("com binários: %v", err)
	}
	os.Remove(filepath.Join(dir, "ffmpeg"))
	if err := Available("yt-dlp"); err == nil || !strings.Contains(err.Error(), "ffmpeg") {
		t.Fatalf("sem ffmpeg: %v", err)
	}
}

func TestSelfUpdate(t *testing.T) {
	y := NewYtDlp("yt-dlp", time.Minute)
	var args []string
	y.run = func(_ context.Context, name string, a ...string) ([]byte, error) {
		args = a
		return []byte("Updated yt-dlp to 2026.09.01\n"), nil
	}
	if err := y.SelfUpdate(context.Background()); err != nil || len(args) != 1 || args[0] != "-U" {
		t.Fatalf("%v %v", err, args)
	}
	y.run = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("ERROR: no write permission"), errors.New("exit 1")
	}
	if err := y.SelfUpdate(context.Background()); err == nil || !strings.Contains(err.Error(), "no write permission") {
		t.Fatalf("%v", err)
	}
}

func TestMetadataReadsTitleDescriptionUploaderAndTags(t *testing.T) {
	var dl []string
	// o yt-dlp imprime o JSON em uma linha; quebras da descrição vêm como \n escapado
	meta := `{"duration": 251.0, "title": " Pão de queijo ", "is_live": false, "description": "Ingredientes:\n500g de polvilho\n3 ovos", "uploader": "Ediane - Comida Mineira", "tags": ["receita", "pão de queijo"], "categories": ["Howto & Style"]}`
	y := newFake(meta, nil, &dl)
	m, err := y.Metadata(context.Background(), mustURL(t, "https://youtu.be/x"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "Pão de queijo" || m.Uploader != "Ediane - Comida Mineira" || m.Duration != 251*time.Second ||
		!strings.Contains(m.Description, "500g de polvilho") || len(m.Tags) != 2 || m.Categories[0] != "Howto & Style" {
		t.Fatalf("%+v", m)
	}
	if len(dl) != 0 {
		t.Fatal("Metadata não pode baixar nada")
	}
}

// Ao vivo não tem duração, mas título e descrição existem: Metadata devolve, Audio recusa.
func TestMetadataWorksForLiveStreamsWhileAudioRefuses(t *testing.T) {
	var dl []string
	live := `{"duration": null, "title": "Rádio lofi 24h", "is_live": true, "description": "Música para estudar", "uploader": "Lofi", "tags": null}`
	y := newFake(live, nil, &dl)
	m, err := y.Metadata(context.Background(), mustURL(t, "https://youtu.be/x"))
	if err != nil || m.Title != "Rádio lofi 24h" || m.Duration != 0 || m.Description != "Música para estudar" {
		t.Fatalf("%+v %v", m, err)
	}
	if _, err := y.Audio(context.Background(), mustURL(t, "https://youtu.be/x"), nil); !errors.Is(err, ErrNoDuration) {
		t.Fatalf("Audio deveria recusar transmissão ao vivo: %v", err)
	}
}

func TestMetadataFailures(t *testing.T) {
	y := NewYtDlp("yt-dlp", time.Minute)
	y.run = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("ERROR: No video could be found in this tweet"), errors.New("exit 1")
	}
	if _, err := y.Metadata(context.Background(), mustURL(t, "https://x.com/u/status/1")); err == nil || !strings.Contains(err.Error(), "No video") {
		t.Fatalf("%v", err)
	}
	y.run = func(context.Context, string, ...string) ([]byte, error) { return []byte("não é json"), nil }
	if _, err := y.Metadata(context.Background(), mustURL(t, "https://youtu.be/x")); err == nil || !strings.Contains(err.Error(), "ilegíveis") {
		t.Fatalf("%v", err)
	}
}
