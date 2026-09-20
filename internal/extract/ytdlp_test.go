package extract

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeRun imita o yt-dlp: --print devolve metadados; o download grava o mp3 no -o.
func fakeRun(meta string, audio []byte, downloads *[]string) runner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
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
				return nil, os.WriteFile(filepath.Join(dir, "audio.mp3"), audio, 0o600)
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
	af, err := y.Audio(context.Background(), mustURL(t, "https://youtu.be/x"))
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
	_, err := y.Audio(context.Background(), mustURL(t, "https://youtu.be/x"))
	var tl *TooLongError
	if !errors.As(err, &tl) || len(dl) != 0 {
		t.Fatalf("err=%v downloads=%v", err, dl)
	}
}

func TestAudioLiveOrNoDuration(t *testing.T) {
	var dl []string
	for _, meta := range []string{`{"duration": null, "title": "x", "is_live": true}`, `{"duration": null, "title": "x", "is_live": false}`} {
		y := newFake(meta, nil, &dl)
		if _, err := y.Audio(context.Background(), mustURL(t, "https://youtu.be/x")); !errors.Is(err, ErrNoDuration) {
			t.Fatalf("%s: %v", meta, err)
		}
	}
}

func TestAudioTooLargeAndCleanup(t *testing.T) {
	var dl []string
	y := newFake(`{"duration": 60, "title": "x", "is_live": false}`, make([]byte, maxAudioBytes+1), &dl)
	_, err := y.Audio(context.Background(), mustURL(t, "https://youtu.be/x"))
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
	y.Audio(context.Background(), mustURL(t, "https://youtu.be/--exec=evil"))
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
	af, err := y.Audio(context.Background(), mustURL(t, "https://youtu.be/x"))
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
	if _, err := y.Audio(context.Background(), mustURL(t, "https://youtu.be/x")); err == nil || !strings.Contains(err.Error(), "ffmpeg") {
		t.Fatal(err)
	}
}
