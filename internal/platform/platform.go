// Package platform detecta a plataforma de um link e calcula a URL canônica.
package platform

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// Nomes das plataformas reconhecidas (valor da coluna items.platform).
const (
	YouTube   = "youtube"
	TikTok    = "tiktok"
	X         = "x"
	Instagram = "instagram"
	LinkedIn  = "linkedin"
	Other     = "other"
)

// Extract diz quais plataformas têm extração de áudio.
// TikTok e X são "melhor esforço": o yt-dlp pode falhar sem aviso (IP bloqueado,
// post protegido) e, nesse caso, o bot cai no pedido de descrição.
var Extract = map[string]bool{
	YouTube:   true,
	TikTok:    true,
	X:         true,
	Instagram: false,
	LinkedIn:  false,
	Other:     false,
}

var instaUserPath = regexp.MustCompile(`^/[^/]+/(p|reel|reels|tv)/([^/]+)$`)

var urlRe = regexp.MustCompile(`https?://[^\s<>"]+`)

// FindURL devolve o primeiro link do texto e o restante do texto sem ele.
func FindURL(text string) (link, rest string, ok bool) {
	loc := urlRe.FindStringIndex(text)
	if loc == nil {
		return "", strings.TrimSpace(text), false
	}
	link = strings.TrimRight(text[loc[0]:loc[1]], ".,;:!?)")
	rest = strings.Join(strings.Fields(text[:loc[0]]+" "+text[loc[0]+len(link):]), " ")
	return link, rest, true
}

// Canonical devolve a plataforma e a URL canônica usada para detectar duplicatas.
func Canonical(raw string) (plat, canonical string, err error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", "", errors.New("link inválido")
	}
	host := strings.ToLower(u.Hostname())
	for _, p := range []string{"www.", "m.", "mobile."} {
		host = strings.TrimPrefix(host, p)
	}
	path := strings.TrimRight(u.EscapedPath(), "/")

	switch {
	case host == "youtu.be":
		if id := strings.TrimPrefix(path, "/"); id != "" {
			return YouTube, youtube(id), nil
		}
	case host == "youtube.com" || host == "music.youtube.com":
		if id := u.Query().Get("v"); id != "" && path == "/watch" {
			return YouTube, youtube(id), nil
		}
		for _, p := range []string{"/shorts/", "/live/", "/embed/"} {
			if id, ok := strings.CutPrefix(path, p); ok && id != "" {
				return YouTube, youtube(id), nil
			}
		}
		return YouTube, build("youtube.com", path, keep(u.Query(), "v", "list")), nil
	case host == "tiktok.com" || strings.HasSuffix(host, ".tiktok.com"):
		return TikTok, build(host, path, nil), nil
	case host == "x.com" || host == "twitter.com":
		return X, build("x.com", path, nil), nil
	case host == "linkedin.com" || strings.HasSuffix(host, ".linkedin.com"):
		// pt., br. etc. abrem o mesmo post; os parâmetros são só rastreio.
		return LinkedIn, build("linkedin.com", path, nil), nil
	case host == "instagram.com" || host == "instagr.am":
		// /usuario/p/<código> redireciona para /p/<código>: é o mesmo post.
		if m := instaUserPath.FindStringSubmatch(path); m != nil {
			path = "/" + m[1] + "/" + m[2]
		}
		return Instagram, build("instagram.com", path, nil), nil
	}
	return Other, build(host, path, stripTracking(u.Query())), nil
}

func youtube(id string) string {
	return build("youtube.com", "/watch", url.Values{"v": {id}})
}

func build(host, path string, q url.Values) string {
	u := url.URL{Scheme: "https", Host: host, Path: path, RawQuery: q.Encode()}
	if p, err := url.PathUnescape(path); err == nil {
		u.Path = p
	}
	return u.String()
}

func keep(q url.Values, keys ...string) url.Values {
	out := url.Values{}
	for _, k := range keys {
		if v, ok := q[k]; ok {
			out[k] = v
		}
	}
	return out
}

func stripTracking(q url.Values) url.Values {
	out := url.Values{}
	for k, v := range q {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "utm_") || lk == "igshid" || lk == "si" || lk == "fbclid" || lk == "gclid" {
			continue
		}
		out[k] = v
	}
	return out
}
