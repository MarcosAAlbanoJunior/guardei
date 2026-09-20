package search

import (
	"testing"
	"time"
)

// quarta-feira, 2026-09-16, 15:30 em Brasília (UTC-3)
var now = time.Date(2026, 9, 16, 15, 30, 0, 0, time.FixedZone("BRT", -3*3600))

func TestParseQueryPeriods(t *testing.T) {
	midnight := time.Date(2026, 9, 16, 0, 0, 0, 0, now.Location())
	days := func(n int) time.Time { return now.AddDate(0, 0, -n) }

	for _, c := range []struct {
		in         string
		text       string
		since      time.Time
		until      time.Time
		periodName string
	}{
		{"receitas de hoje", "receitas", midnight, time.Time{}, "hoje"},
		{"hoje receitas", "receitas", midnight, time.Time{}, "hoje"},
		{"o que eu salvei hoje", "", midnight, time.Time{}, "hoje"},
		{"receitas de ontem", "receitas", midnight.AddDate(0, 0, -1), midnight, "ontem"},
		{"receitas da semana", "receitas", days(7), time.Time{}, "últimos 7 dias"},
		{"vídeos dessa semana", "", days(7), time.Time{}, "últimos 7 dias"},
		{"receita essa semana", "receita", days(7), time.Time{}, "últimos 7 dias"},
		{"receita na semana passada", "receita", days(14), days(7), "semana passada"},
		{"receita da semana passada", "receita", days(14), days(7), "semana passada"},
		{"bolo deste mês", "bolo", days(30), time.Time{}, "últimos 30 dias"},
		{"bolo do mês", "bolo", days(30), time.Time{}, "últimos 30 dias"},
		{"bolo mês passado", "bolo", days(60), days(30), "mês passado"},
		{"pizza últimos 15 dias", "pizza", days(15), time.Time{}, "últimos 15 dias"},
		{"pizza ultimas 2 semanas", "pizza", days(14), time.Time{}, "últimos 14 dias"},
		{"pizza últimos 3 meses", "pizza", days(90), time.Time{}, "últimos 90 dias"},
		{"HOJE", "", midnight, time.Time{}, "hoje"},
		{"Receitas de hoje!", "Receitas", midnight, time.Time{}, "hoje"},
		{"o que eu vi ontem", "", midnight.AddDate(0, 0, -1), midnight, "ontem"},
	} {
		q := ParseQuery(c.in, now)
		if q.Text != c.text || !q.Filter.Since.Equal(c.since) || !q.Filter.Until.Equal(c.until) || q.period != c.periodName {
			t.Errorf("%q -> texto=%q desde=%v até=%v período=%q\n  queria texto=%q desde=%v até=%v período=%q",
				c.in, q.Text, q.Filter.Since, q.Filter.Until, q.period, c.text, c.since, c.until, c.periodName)
		}
	}
}

func TestParseQueryPlatforms(t *testing.T) {
	for _, c := range []struct{ in, text, platform, label string }{
		{"receitas do youtube", "receitas", "youtube", "YouTube"},
		{"só do youtube", "", "youtube", "YouTube"},
		{"bolo no tiktok", "bolo", "tiktok", "TikTok"},
		{"apenas instagram", "", "instagram", "Instagram"},
		{"posts do linkedin", "", "linkedin", "LinkedIn"},
		{"css no x", "css", "x", "X"},
		{"tweets do twitter", "tweets", "x", "X"},
		{"tiktok de bolo", "bolo", "tiktok", "TikTok"},
	} {
		q := ParseQuery(c.in, now)
		if q.Text != c.text || q.Filter.Platform != c.platform || q.Label() != c.label {
			t.Errorf("%q -> texto=%q plataforma=%q rótulo=%q (queria %q %q %q)", c.in, q.Text, q.Filter.Platform, q.Label(), c.text, c.platform, c.label)
		}
	}
}

func TestParseQueryCombined(t *testing.T) {
	q := ParseQuery("receitas do youtube da semana", now)
	if q.Text != "receitas" || q.Filter.Platform != "youtube" || q.Label() != "YouTube · últimos 7 dias" || !q.HasPeriod() {
		t.Fatalf("%+v %q", q, q.Label())
	}
	q = ParseQuery("receitas do youtube hoje", now) // encostado em outro filtro
	if q.Text != "receitas" || q.Label() != "YouTube · hoje" {
		t.Fatalf("%+v %q", q, q.Label())
	}
	q = ParseQuery("só youtube hoje", now)
	if q.Text != "" || q.Label() != "YouTube · hoje" {
		t.Fatalf("%+v %q", q, q.Label())
	}
}

// Sem filtro reconhecido, a mensagem passa intacta.
func TestParseQueryLeavesPlainSearchesAlone(t *testing.T) {
	for _, in := range []string{
		"receita de bolo", "como fazer pão de queijo", "o que eu preciso aprender", "x", "dicas de css",
		"semana", "mês", "últimos", "últimos dias", "ultimos abc dias", "quero ver", "hojeeee", "explicação do x11",
		// "hoje" e "ontem" no meio do assunto não são filtro
		"algo para cozinhar hoje", "receitas hoje", "o que cozinhar ontem à noite", "bolo para hoje",
	} {
		q := ParseQuery(in, now)
		if q.Text != in || !q.Filter.IsZero() || q.Label() != "" {
			t.Errorf("%q foi alterada: %+v", in, q)
		}
	}
}

func TestWithPeriod(t *testing.T) {
	base := ParseQuery("receitas do youtube hoje", now)
	week := base.WithPeriod(7, now)
	if week.Label() != "YouTube · últimos 7 dias" || week.Text != "receitas" || week.Filter.Platform != "youtube" {
		t.Fatalf("%+v %q", week, week.Label())
	}
	all := base.WithPeriod(0, now)
	if all.HasPeriod() || all.Label() != "YouTube" {
		t.Fatalf("todo o período deve manter a plataforma: %+v %q", all, all.Label())
	}
	if d := base.WithPeriod(1, now); !d.Filter.Since.Equal(time.Date(2026, 9, 16, 0, 0, 0, 0, now.Location())) {
		t.Fatalf("hoje: %v", d.Filter.Since)
	}
	if base.Filter.Since.IsZero() {
		t.Fatal("WithPeriod não pode alterar a consulta original")
	}
}
