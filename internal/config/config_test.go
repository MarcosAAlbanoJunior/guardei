package config

import (
	"strings"
	"testing"
)

// env limpa as variáveis do bot e aplica as informadas.
func env(t *testing.T, kv map[string]string) {
	t.Helper()
	for _, k := range []string{"TELEGRAM_BOT_TOKEN", "ALLOWED_USER_IDS", "DB_PATH", "SEARCH_LIMIT",
		"GEMINI_API_KEY", "GEMINI_MODEL", "GEMINI_EMBEDDING_MODEL", "MAX_VIDEO_SECONDS", "YTDLP_PATH"} {
		t.Setenv(k, "")
	}
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func TestDefaults(t *testing.T) {
	env(t, map[string]string{"TELEGRAM_BOT_TOKEN": "tok", "ALLOWED_USER_IDS": "1, 2 ,3"})
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.TelegramToken != "tok" || len(c.AllowedUsers) != 3 || !c.AllowedUsers[2] {
		t.Errorf("%+v", c)
	}
	if c.DBPath != "./data/app.db" || c.SearchLimit != 5 || c.MaxVideoSeconds != 600 || c.YtdlpPath != "yt-dlp" {
		t.Errorf("padrões: %+v", c)
	}
	if c.GeminiAPIKey != "" || c.GeminiModel != "gemini-2.5-flash-lite" || c.GeminiEmbeddingModel != "gemini-embedding-2" {
		t.Errorf("gemini: %+v", c)
	}
}

func TestOverrides(t *testing.T) {
	env(t, map[string]string{"TELEGRAM_BOT_TOKEN": "tok", "ALLOWED_USER_IDS": "9", "DB_PATH": "/x/a.db",
		"SEARCH_LIMIT": "8", "MAX_VIDEO_SECONDS": "1800", "GEMINI_API_KEY": "k", "GEMINI_MODEL": "m", "YTDLP_PATH": "/bin/yt"})
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.DBPath != "/x/a.db" || c.SearchLimit != 8 || c.MaxVideoSeconds != 1800 || c.GeminiAPIKey != "k" ||
		c.GeminiModel != "m" || c.YtdlpPath != "/bin/yt" {
		t.Errorf("%+v", c)
	}
}

func TestInvalid(t *testing.T) {
	for name, tc := range map[string]struct {
		kv   map[string]string
		want string
	}{
		"sem token":        {map[string]string{"ALLOWED_USER_IDS": "1"}, "TELEGRAM_BOT_TOKEN"},
		"sem usuários":     {map[string]string{"TELEGRAM_BOT_TOKEN": "t"}, "ALLOWED_USER_IDS"},
		"id não numérico":  {map[string]string{"TELEGRAM_BOT_TOKEN": "t", "ALLOWED_USER_IDS": "1,abc"}, "abc"},
		"limite inválido":  {map[string]string{"TELEGRAM_BOT_TOKEN": "t", "ALLOWED_USER_IDS": "1", "SEARCH_LIMIT": "0"}, "SEARCH_LIMIT"},
		"duração inválida": {map[string]string{"TELEGRAM_BOT_TOKEN": "t", "ALLOWED_USER_IDS": "1", "MAX_VIDEO_SECONDS": "x"}, "MAX_VIDEO_SECONDS"},
		"só vírgulas":      {map[string]string{"TELEGRAM_BOT_TOKEN": "t", "ALLOWED_USER_IDS": " , ,"}, "ALLOWED_USER_IDS"},
	} {
		env(t, tc.kv)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v (queria menção a %q)", name, err, tc.want)
		}
	}
}
