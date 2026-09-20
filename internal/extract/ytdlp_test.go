package extract

import (
	"context"
	"errors"
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
	if af.Title != "Me at the zoo" || af.Mime != "audio/mp3" || af.Duration != 19*time.Second || string(af.Data) != "mp3" {
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
