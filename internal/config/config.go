// Package config lê a configuração das variáveis de ambiente.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config é a configuração do bot, lida das variáveis de ambiente.
type Config struct {
	TelegramToken string
	AllowedUsers  map[int64]bool
	DBPath        string
	SearchLimit   int

	GeminiAPIKey         string // vazio liga o modo manual
	GeminiModel          string
	GeminiEmbeddingModel string

	MaxVideoSeconds int
	YtdlpPath       string
}

// Load lê o ambiente. Só TELEGRAM_BOT_TOKEN e ALLOWED_USER_IDS são obrigatórias.
func Load() (Config, error) {
	c := Config{
		TelegramToken: os.Getenv("TELEGRAM_BOT_TOKEN"),
		DBPath:        envOr("DB_PATH", "./data/app.db"),

		GeminiAPIKey:         os.Getenv("GEMINI_API_KEY"),
		GeminiModel:          envOr("GEMINI_MODEL", "gemini-2.5-flash-lite"),
		GeminiEmbeddingModel: envOr("GEMINI_EMBEDDING_MODEL", "gemini-embedding-2"),
		YtdlpPath:            envOr("YTDLP_PATH", "yt-dlp"),
		AllowedUsers:         map[int64]bool{},
	}
	if c.TelegramToken == "" {
		return c, errors.New("TELEGRAM_BOT_TOKEN é obrigatória")
	}
	for _, s := range strings.Split(os.Getenv("ALLOWED_USER_IDS"), ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return c, fmt.Errorf("ALLOWED_USER_IDS inválida: %q", s)
		}
		c.AllowedUsers[id] = true
	}
	if len(c.AllowedUsers) == 0 {
		return c, errors.New("ALLOWED_USER_IDS é obrigatória")
	}
	var err error
	if c.SearchLimit, err = envInt("SEARCH_LIMIT", 5); err != nil {
		return c, err
	}
	if c.MaxVideoSeconds, err = envInt("MAX_VIDEO_SECONDS", 600); err != nil {
		return c, err
	}
	return c, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s inválida: %q", key, v)
	}
	return n, nil
}
