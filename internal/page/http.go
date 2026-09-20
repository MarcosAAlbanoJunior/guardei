package page

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

const (
	readTimeout = 15 * time.Second
	maxBody     = 1 << 20 // o que importa (head e primeiros parágrafos) está no começo
	maxRedirect = 5

	// browserUA serve à maioria dos sites (LinkedIn e X entregam o texto do post a ele).
	browserUA = "Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0"
	// previewUA: o Instagram só devolve a legenda ao rastreador de preview de links do
	// Facebook (o mesmo usado por WhatsApp e Telegram para mostrar prévias). Só é usado nele.
	previewUA = "facebookexternalhit/1.1 (+http://www.facebook.com/externalhit_uatext.php)"
)

// HTTPReader busca a página com um GET por link salvo, a pedido do usuário.
type HTTPReader struct {
	client *http.Client
}

// NewHTTP devolve o leitor padrão. Ele nunca conecta em endereços internos
// (loopback, redes privadas, link-local), nem via redirecionamento.
func NewHTTP() *HTTPReader { return newHTTP(true) }

func newHTTP(blockInternal bool) *HTTPReader {
	d := &net.Dialer{Timeout: 10 * time.Second}
	if blockInternal {
		d.Control = refuseInternal
	}
	tr := &http.Transport{
		DialContext:           d.DialContext,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		// Sem proxy do ambiente: o filtro acima precisa ver o destino real.
	}
	return &HTTPReader{client: &http.Client{
		Transport: tr,
		Timeout:   readTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirect {
				return errors.New("redirecionamentos demais")
			}
			return checkScheme(req.URL)
		},
	}}
}

func checkScheme(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("esquema %q não permitido", u.Scheme)
	}
	return nil
}

// refuseInternal roda depois da resolução de DNS, na hora de conectar: vale
// também contra DNS rebinding e redirecionamentos para dentro da rede.
func refuseInternal(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || isInternal(ip) {
		return fmt.Errorf("endereço interno recusado: %s", host)
	}
	return nil
}

var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

func isInternal(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() || cgnat.Contains(ip)
}

func (r *HTTPReader) Read(ctx context.Context, rawURL string) (Page, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return Page{}, fmt.Errorf("link inválido")
	}
	if err := checkScheme(u); err != nil {
		return Page{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Page{}, err
	}
	req.Header.Set("User-Agent", userAgent(u.Hostname()))
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.5")
	req.Header.Set("Accept-Language", "pt-BR,pt;q=0.9,en;q=0.7")

	res, err := r.client.Do(req)
	if err != nil {
		return Page{}, fmt.Errorf("não consegui abrir a página: %w", err)
	}
	defer res.Body.Close()

	switch {
	case res.StatusCode == http.StatusOK:
	case res.StatusCode == 401 || res.StatusCode == 403 || res.StatusCode == 404 || res.StatusCode == 429 || res.StatusCode == 999:
		// 999 é o "bloqueado" do LinkedIn.
		return Page{}, fmt.Errorf("%w (HTTP %d)", ErrBlocked, res.StatusCode)
	default:
		return Page{}, fmt.Errorf("a página respondeu HTTP %d", res.StatusCode)
	}
	if looksLikeLogin(res.Request.URL) {
		return Page{}, fmt.Errorf("%w (redirecionou para login)", ErrBlocked)
	}
	if mt, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type")); mt != "text/html" && mt != "application/xhtml+xml" {
		return Page{}, fmt.Errorf("%w (o link não é uma página HTML)", ErrNoContent)
	}

	body, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return Page{}, err
	}
	p, err := parse(body, res.Request.URL, isSocial(u.Hostname()))
	if err != nil {
		return Page{}, err
	}
	p.URL = res.Request.URL.String()
	return p, nil
}

func userAgent(host string) string {
	if hostIs(host, "instagram.com") {
		return previewUA
	}
	return browserUA
}

// isSocial: nessas plataformas o texto do post está nas tags de metadados; o
// corpo do HTML é só interface e não deve entrar.
func isSocial(host string) bool {
	return hostIs(host, "instagram.com") || hostIs(host, "linkedin.com") || hostIs(host, "x.com") || hostIs(host, "twitter.com")
}

func hostIs(host, domain string) bool {
	host = strings.ToLower(host)
	return host == domain || strings.HasSuffix(host, "."+domain)
}

func looksLikeLogin(u *url.URL) bool {
	p := strings.ToLower(u.Path)
	return strings.Contains(p, "/login") || strings.Contains(p, "/accounts/login") ||
		strings.Contains(p, "/authwall") || strings.Contains(p, "/uas/login") || strings.Contains(p, "/i/flow/login")
}
