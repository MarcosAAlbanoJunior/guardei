// Command bot é o Guardei: um bot de Telegram que salva vídeos e posts e os
// devolve por busca em linguagem natural. Veja o README para a configuração.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/MarcosAAlbanoJunior/guardei/internal/ai"
	"github.com/MarcosAAlbanoJunior/guardei/internal/bot"
	"github.com/MarcosAAlbanoJunior/guardei/internal/config"
	"github.com/MarcosAAlbanoJunior/guardei/internal/extract"
	"github.com/MarcosAAlbanoJunior/guardei/internal/page"
	"github.com/MarcosAAlbanoJunior/guardei/internal/search"
	"github.com/MarcosAAlbanoJunior/guardei/internal/store"
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
	extraction := false
	if err := extract.Available(cfg.YtdlpPath); err != nil {
		slog.Warn("extração de áudio desligada; o bot pedirá descrição", "motivo", err)
	} else {
		yt := extract.NewYtDlp(cfg.YtdlpPath, time.Duration(cfg.MaxVideoSeconds)*time.Second)
		extractor, extraction = yt, true
		go yt.UpdateLoop(ctx, 7*24*time.Hour) // yt-dlp quebra quando a plataforma muda
	}

	h := &bot.Handler{
		Store:       st,
		AI:          client,
		Searcher:    &search.Searcher{Store: st, AI: client, Index: index},
		Extractor:   extractor,
		Pages:       page.NewHTTP(),
		AIInfo:      cfg.GeminiModel + " + " + cfg.GeminiEmbeddingModel,
		Allowed:     cfg.AllowedUsers,
		SearchLimit: cfg.SearchLimit,
	}
	slog.Info("bot iniciado", "db", cfg.DBPath, "ia", client.Enabled(), "vetores", index.Len(), "extracao", extraction)
	return bot.Run(ctx, cfg.TelegramToken, h)
}
