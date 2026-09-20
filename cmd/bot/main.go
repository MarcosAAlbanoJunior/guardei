package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/marcosjunior/guardei/internal/bot"
	"github.com/marcosjunior/guardei/internal/config"
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

	h := &bot.Handler{Store: st, Allowed: cfg.AllowedUsers, SearchLimit: cfg.SearchLimit}
	slog.Info("bot iniciado", "db", cfg.DBPath)
	return bot.Run(ctx, cfg.TelegramToken, h)
}
