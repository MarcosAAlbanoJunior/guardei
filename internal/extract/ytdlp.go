package extract

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/marcosjunior/guardei/internal/platform"
)

const (
	probeTimeout    = 30 * time.Second
	downloadTimeout = 60 * time.Second
	// maxAudioBytes deixa folga no limite de 20 MB da requisição inline do Gemini.
	maxAudioBytes = 15 << 20

	// segmentDuration: pedaços de 5 min saem completos; corridas únicas de ~25
	// min pararam na metade ou entraram em repetição.
	segmentDuration = 300 * time.Second
	splitTimeout    = 60 * time.Second
)

// YtDlp extrai o áudio com yt-dlp (download) e ffmpeg (conversão para mp3 mono 16 kHz, ~32 kbps).
type YtDlp struct {
	Path        string
	FFmpegPath  string
	MaxDuration time.Duration

	sem chan struct{}
	run runner
}

type runner func(ctx context.Context, name string, args ...string) ([]byte, error)

func NewYtDlp(path string, maxDuration time.Duration) *YtDlp {
	return &YtDlp{Path: path, FFmpegPath: "ffmpeg", MaxDuration: maxDuration,
		// Uma extração por vez: mantém a RAM baixa em VPS de 512 MB.
		sem: make(chan struct{}, 1), run: execRun}
}

func execRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.Bytes(), err
}

// Available diz se yt-dlp e ffmpeg foram encontrados.
func Available(ytdlpPath string) error {
	for _, bin := range []string{ytdlpPath, "ffmpeg"} {
		if _, err := exec.LookPath(bin); err != nil {
			return fmt.Errorf("%s não encontrado no PATH", bin)
		}
	}
	return nil
}

func (y *YtDlp) Supports(u *url.URL) bool {
	plat, _, err := platform.Canonical(u.String())
	return err == nil && platform.Extract[plat]
}

func (y *YtDlp) Audio(ctx context.Context, u *url.URL) (AudioFile, error) {
	select {
	case y.sem <- struct{}{}:
		defer func() { <-y.sem }()
	case <-ctx.Done():
		return AudioFile{}, ctx.Err()
	}

	title, dur, err := y.probe(ctx, u.String())
	if err != nil {
		return AudioFile{}, err
	}
	if dur > y.MaxDuration {
		return AudioFile{}, &TooLongError{Duration: dur, Max: y.MaxDuration}
	}

	dir, err := os.MkdirTemp("", "guardei-*")
	if err != nil {
		return AudioFile{}, err
	}
	defer os.RemoveAll(dir) // sucesso ou erro

	dctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	out, err := y.run(dctx, y.Path,
		"--no-playlist", "--no-warnings", "--no-progress",
		"--max-filesize", "50M",
		"-f", "bestaudio/best", "-x", "--audio-format", "mp3",
		"--postprocessor-args", "ExtractAudio:-ac 1 -ar 16000 -b:a 32k",
		"-o", filepath.Join(dir, "audio.%(ext)s"),
		"--", u.String())
	if err != nil {
		return AudioFile{}, fmt.Errorf("yt-dlp: %w: %s", err, lastLine(out))
	}

	segments, err := y.segments(ctx, dir, dur)
	if err != nil {
		return AudioFile{}, err
	}
	return AudioFile{Segments: segments, Mime: "audio/mp3", Duration: dur, Title: title}, nil
}

// segments lê o mp3 baixado e, se for longo, o fatia em pedaços sem reencodar.
func (y *YtDlp) segments(ctx context.Context, dir string, dur time.Duration) ([][]byte, error) {
	src := filepath.Join(dir, "audio.mp3")
	if info, err := os.Stat(src); err != nil {
		return nil, fmt.Errorf("áudio não gerado: %w", err)
	} else if info.Size() > maxAudioBytes {
		return nil, ErrTooLarge
	}

	files := []string{src}
	if dur > segmentDuration+segmentDuration/10 {
		sctx, cancel := context.WithTimeout(ctx, splitTimeout)
		defer cancel()
		out, err := y.run(sctx, y.FFmpegPath, "-loglevel", "error", "-i", src,
			"-f", "segment", "-segment_time", fmt.Sprint(int(segmentDuration.Seconds())),
			"-c", "copy", filepath.Join(dir, "seg_%03d.mp3"))
		if err != nil {
			return nil, fmt.Errorf("ffmpeg: %w: %s", err, lastLine(out))
		}
		if files, err = filepath.Glob(filepath.Join(dir, "seg_*.mp3")); err != nil || len(files) == 0 {
			return nil, fmt.Errorf("ffmpeg não gerou trechos")
		}
		sort.Strings(files)
	}

	segs := make([][]byte, 0, len(files))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		segs = append(segs, b)
	}
	return segs, nil
}

func (y *YtDlp) probe(ctx context.Context, rawURL string) (title string, dur time.Duration, err error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out, err := y.run(ctx, y.Path, "--no-playlist", "--no-warnings",
		"--print", "%(.{duration,title,is_live})j", "--", rawURL)
	if err != nil {
		return "", 0, fmt.Errorf("yt-dlp: %w: %s", err, lastLine(out))
	}
	var meta struct {
		Duration *float64 `json:"duration"`
		Title    string   `json:"title"`
		IsLive   bool     `json:"is_live"`
	}
	// A saída pode ter linhas extras antes do JSON; usa a última linha.
	if err := json.Unmarshal([]byte(lastLine(out)), &meta); err != nil {
		return "", 0, fmt.Errorf("metadados ilegíveis: %w", err)
	}
	if meta.IsLive || meta.Duration == nil || *meta.Duration <= 0 {
		return "", 0, ErrNoDuration
	}
	return strings.TrimSpace(meta.Title), time.Duration(*meta.Duration * float64(time.Second)), nil
}

// SelfUpdate roda `yt-dlp -U`: ele quebra sempre que uma plataforma muda.
func (y *YtDlp) SelfUpdate(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	out, err := y.run(ctx, y.Path, "-U")
	if err != nil {
		return fmt.Errorf("yt-dlp -U: %w: %s", err, lastLine(out))
	}
	slog.Info("yt-dlp atualizado", "saida", lastLine(out))
	return nil
}

// UpdateLoop atualiza o yt-dlp a cada interval, até ctx terminar.
func (y *YtDlp) UpdateLoop(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := y.SelfUpdate(ctx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Warn("atualização do yt-dlp falhou", "err", err)
			}
		}
	}
}

func lastLine(b []byte) string {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
