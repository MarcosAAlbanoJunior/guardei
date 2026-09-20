package main

import (
	"context"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/marcosjunior/guardei/internal/ai"
	"github.com/marcosjunior/guardei/internal/bot"
	"github.com/marcosjunior/guardei/internal/config"
	"github.com/marcosjunior/guardei/internal/extract"
	"github.com/marcosjunior/guardei/internal/search"
	"github.com/marcosjunior/guardei/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.Error("encerrando", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	client := ai.New(cfg.GeminiAPIKey, cfg.GeminiModel, cfg.GeminiEmbeddingModel)
	index := search.NewIndex()
	if err := index.Load(ctx, st); err != nil {
		return err
	}

	var extractor extract.Extractor = extract.Nop{}
	if err := extract.Available(cfg.YtdlpPath); err != nil {
		slog.Warn("extração de áudio desligada; o bot pedirá descrição", "motivo", err)
	} else {
		yt := extract.NewYtDlp(cfg.YtdlpPath, time.Duration(cfg.MaxVideoSeconds)*time.Second)
		extractor = yt
		go yt.UpdateLoop(ctx, 7*24*time.Hour) // yt-dlp quebra quando a plataforma muda
	}

	h := &bot.Handler{
		Store:       st,
		AI:          client,
		Searcher:    &search.Searcher{Store: st, AI: client, Index: index},
		Extractor:   extractor,
		AIInfo:      cfg.GeminiModel + " + " + cfg.GeminiEmbeddingModel,
		Allowed:     cfg.AllowedUsers,
		SearchLimit: cfg.SearchLimit,
	}
	slog.Info("bot iniciado", "db", cfg.DBPath, "ia", client.Enabled(), "vetores", index.Len(), "extracao", extractor.Supports(mustParse("https://youtu.be/x")))
	return bot.Run(ctx, cfg.TelegramToken, h)
}

func mustParse(raw string) *url.URL {
	u, _ := url.Parse(raw)
	return u
}
