package bot

import (
	"strings"
	"testing"
	"time"
)

func TestShorten(t *testing.T) {
	if got := shorten("  muitos   espaços\naqui  ", 50); got != "muitos espaços aqui" {
		t.Errorf("%q", got)
	}
	got := shorten("abcdefghij", 5)
	if got != "abcd…" || len([]rune(got)) != 5 {
		t.Errorf("deve caber em n caracteres, contando o …: %q", got)
	}
	if got := shorten("pão de queijo", 4); got != "pão…" { // não corta no meio de um caractere
		t.Errorf("%q", got)
	}
}

func TestSavedMessage(t *testing.T) {
	got := savedMessage(3, "youtube", "transcrito", "Título", "Resumo.", []string{"a", "b"})
	if got != "Salvo (#3, youtube, transcrito):\nTítulo\nResumo.\nTags: a, b" {
		t.Errorf("%q", got)
	}
	if got := savedMessage(1, "other", "", "", "pão de queijo", nil); got != "Salvo (#1, other):\npão de queijo" {
		t.Errorf("%q", got)
	}
	long := strings.Repeat("palavra ", 100)
	if n := len([]rune(savedMessage(1, "x", "", "", long, nil))); n > 60+maxSummary {
		t.Errorf("resumo não foi limitado: %d", n)
	}
}

func TestParseID(t *testing.T) {
	for in, want := range map[string]int64{"12": 12, " #7 ": 7} {
		if got, err := parseID(in); err != nil || got != want {
			t.Errorf("%q: %d %v", in, got, err)
		}
	}
	if _, err := parseID("abc"); err == nil {
		t.Error("esperava erro")
	}
}

func TestAge(t *testing.T) {
	now := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		t    time.Time
		want string
	}{
		{now, "hoje"},
		{now.Add(-9 * time.Hour), "hoje"},   // 01:00 do mesmo dia
		{now.Add(-11 * time.Hour), "ontem"}, // 23:00 de ontem: dia de calendário, não 24 h
		{now.AddDate(0, 0, -1), "ontem"},
		{now.AddDate(0, 0, -5), "há 5 dias"},
		{now.AddDate(0, 0, -13), "há 13 dias"},
		{now.AddDate(0, 0, -14), "há 2 semanas"},
		{now.AddDate(0, 0, -45), "há 6 semanas"},
		{now.AddDate(0, 0, -60), "há 2 meses"},
		{now.AddDate(0, -8, 0), "há 8 meses"},
		{now.Add(time.Hour), "hoje"}, // relógio adiantado: nunca "daqui a"
	} {
		if got := age(c.t, now); got != c.want {
			t.Errorf("%v: %q (queria %q)", c.t, got, c.want)
		}
	}
}

func TestToMarkup(t *testing.T) {
	m := toMarkup([][]Button{{{Label: "Ver mais 5 ▶", Data: "m:3"}}, {{Label: "Hoje", Data: "p:3:1"}, {Label: "7 dias", Data: "p:3:7"}}})
	if len(m.InlineKeyboard) != 2 || len(m.InlineKeyboard[1]) != 2 ||
		m.InlineKeyboard[0][0].Text != "Ver mais 5 ▶" || m.InlineKeyboard[1][1].CallbackData != "p:3:7" {
		t.Fatalf("%+v", m)
	}
	for _, row := range m.InlineKeyboard {
		for _, b := range row {
			if len(b.CallbackData) > 64 { // limite do Telegram
				t.Errorf("callback_data acima de 64 bytes: %q", b.CallbackData)
			}
		}
	}
}
