package bot

import (
	"strings"
	"testing"
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
