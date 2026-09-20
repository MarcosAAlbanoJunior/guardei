package page

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const linkedinHTML = `<!doctype html><html><head><title>LinkedIn</title>
<meta property="og:title" content="Se você é programador front-end, esse post é daqueles">
<meta property="og:description" content="Se você é programador front-end, esse post é daqueles que pode elevar a qualidade do seu trabalho 🚀&#10;&#10;Segue o fio com 10 dicas de CSS.">
<meta property="og:image" content="/img/capa.jpg">
<meta property="og:site_name" content="LinkedIn">
</head><body><nav><p>Menu de navegação que não deve entrar no texto de jeito nenhum</p></nav></body></html>`

func serve(t *testing.T, h http.HandlerFunc) (*HTTPReader, string) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return newHTTP(false), srv.URL // false: o servidor de teste está em 127.0.0.1
}

func html200(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, body)
	}
}

func TestOpenGraphPost(t *testing.T) {
	r, u := serve(t, html200(linkedinHTML))
	p, err := r.Read(context.Background(), u+"/posts/x")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p.Title, "Se você é programador") || p.SiteName != "LinkedIn" ||
		!strings.Contains(p.Text, "10 dicas de CSS") || p.Partial || p.ImageURL != u+"/img/capa.jpg" {
		t.Fatalf("%+v", p)
	}
	if !strings.Contains(p.Text, "\n\n") {
		t.Error("quebra de parágrafo do post foi perdida")
	}
}

func TestPartialWhenPlatformTruncates(t *testing.T) {
	r, u := serve(t, html200(`<head><meta property="og:description" content="Thread pra você entender a maior fofoca da bolha tech de 2026. Cada app, site ou serviço…"></head>`))
	p, err := r.Read(context.Background(), u)
	if err != nil || !p.Partial {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestArticleBodyTextForGenericSites(t *testing.T) {
	r, u := serve(t, html200(`<html><head><title>Como funciona o GC do Go</title>
<meta name="description" content="Uma visão geral do coletor de lixo."></head><body>
<nav><p>Início Sobre Contato Blog Newsletter Assine agora mesmo para receber</p></nav>
<article><h1>Como funciona o GC do Go, por dentro</h1>
<p>O coletor de lixo do Go é concorrente e usa marcação tricolor, com pausas muito curtas.</p>
<p>curto</p>
<script>var x = "isto não é texto do artigo, é código javascript de rastreio";</script>
<p>Ajustar GOGC muda o equilíbrio entre uso de memória e tempo gasto coletando lixo.</p></article>
<footer><p>© 2026 Todos os direitos reservados a este site de exemplo.</p></footer></body></html>`))
	p, err := r.Read(context.Background(), u)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Uma visão geral", "marcação tricolor", "GOGC"} {
		if !strings.Contains(p.Text, want) {
			t.Errorf("faltou %q em %q", want, p.Text)
		}
	}
	for _, bad := range []string{"Newsletter", "javascript", "direitos reservados", "curto"} {
		if strings.Contains(p.Text, bad) {
			t.Errorf("não deveria conter %q: %q", bad, p.Text)
		}
	}
}

func TestSocialIgnoresBodyText(t *testing.T) {
	// no host de uma rede social só valem os metadados; aqui simulamos por isSocial
	p, err := parse([]byte(`<head><meta property="og:description" content="Legenda do post com texto suficiente para passar do mínimo útil."></head>
<body><p>Texto de interface que não faz parte do post e é bem comprido mesmo assim.</p></body>`), mustParse("https://www.instagram.com/p/x/"), true)
	if err != nil || strings.Contains(p.Text, "interface") {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestBlockedAndNoContent(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		h    http.HandlerFunc
		want error
	}{
		"403":   {func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) }, ErrBlocked},
		"999":   {func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(999) }, ErrBlocked},
		"login": {html200(`<head><title>LinkedIn: entre ou cadastre-se</title></head>`), ErrBlocked},
		"redirect": {func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/accounts/login") {
				html200(`<head><title>Página qualquer com texto o bastante para passar</title></head>`)(w, r)
				return
			}
			http.Redirect(w, r, "/accounts/login/?next=/p/x", http.StatusFound)
		}, ErrBlocked},
		"vazia": {html200(`<head><title>Oi</title></head>`), ErrNoContent},
		"nao-html": {func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/pdf")
			fmt.Fprint(w, "%PDF")
		}, ErrNoContent},
	} {
		r, u := serve(t, tc.h)
		if _, err := r.Read(ctx, u+"/p/x"); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v (queria %v)", name, err, tc.want)
		}
	}
}

func TestRefusesInternalAddresses(t *testing.T) {
	srv := httptest.NewServer(html200(linkedinHTML)) // 127.0.0.1
	defer srv.Close()
	if _, err := NewHTTP().Read(context.Background(), srv.URL); err == nil || !strings.Contains(err.Error(), "interno") {
		t.Fatalf("deveria recusar loopback: %v", err)
	}
	// redirecionamento de um site público para dentro da rede: também recusado
	redir := httptest.NewServer(http.RedirectHandler(srv.URL, http.StatusFound))
	defer redir.Close()
	if _, err := NewHTTP().Read(context.Background(), redir.URL); err == nil {
		t.Fatal("deveria recusar")
	}
}

func TestRefusesNonHTTPSchemes(t *testing.T) {
	for _, u := range []string{"file:///etc/passwd", "ftp://x.com/a", "gopher://x"} {
		if _, err := newHTTP(false).Read(context.Background(), u); err == nil {
			t.Errorf("%s deveria falhar", u)
		}
	}
}

func TestUserAgentPerSite(t *testing.T) {
	if userAgent("www.instagram.com") != previewUA || userAgent("pt.linkedin.com") != browserUA || userAgent("x.com") != browserUA {
		t.Fatal("user agents")
	}
}

func TestIsInternal(t *testing.T) {
	for ip, want := range map[string]bool{"127.0.0.1": true, "10.0.0.5": true, "192.168.1.1": true, "172.16.0.1": true,
		"169.254.169.254": true, "100.64.0.1": true, "::1": true, "fd00::1": true, "0.0.0.0": true,
		"8.8.8.8": false, "142.250.0.1": false, "2606:4700::1": false} {
		if got := isInternal(parseIP(ip)); got != want {
			t.Errorf("%s: %v", ip, got)
		}
	}
}

func mustParse(s string) *url.URL { u, _ := url.Parse(s); return u }
func parseIP(s string) net.IP     { return net.ParseIP(s) }
