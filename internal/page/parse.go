package page

import (
	"bytes"
	"io"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
)

const (
	maxText     = 6000
	maxTitle    = 300
	minUseful   = 40 // menos que isso (título + texto) não dá para analisar
	minParagraf = 40 // parágrafos menores costumam ser menu e rodapé
)

var loginWall = regexp.MustCompile(`(?i)(entre ou cadastre-se|faça login|fazer login|log in to|sign in to|sign up|join linkedin|enable javascript|verify you are human|just a moment|access denied|acesso negado)`)

// parse extrai metadados e, fora das redes sociais, o texto dos parágrafos.
func parse(body []byte, base *url.URL, social bool) (Page, error) {
	meta := map[string]string{}
	var title string
	var paras []string

	z := html.NewTokenizer(bytes.NewReader(body))
	var (
		inTitle bool
		skip    int    // dentro de script, style, nav, footer…
		collect string // tag de texto aberta (p, h1, li…)
		buf     strings.Builder
	)
	skipTags := map[string]bool{"script": true, "style": true, "noscript": true, "svg": true, "nav": true,
		"footer": true, "header": true, "aside": true, "form": true, "template": true}
	textTags := map[string]bool{"p": true, "h1": true, "h2": true, "h3": true, "li": true, "blockquote": true}

	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			if z.Err() != io.EOF && len(meta) == 0 && title == "" {
				return Page{}, z.Err()
			}
			break
		}
		switch tt {
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			tag := string(name)
			attrs := map[string]string{}
			for hasAttr {
				var k, v []byte
				k, v, hasAttr = z.TagAttr()
				attrs[strings.ToLower(string(k))] = string(v)
			}
			switch {
			case tag == "meta":
				key := strings.ToLower(firstNonEmpty(attrs["property"], attrs["name"]))
				if key != "" && attrs["content"] != "" {
					if _, seen := meta[key]; !seen {
						meta[key] = attrs["content"]
					}
				}
			case tag == "title" && title == "":
				inTitle = true
			case skipTags[tag] && tt == html.StartTagToken:
				skip++
			case textTags[tag] && skip == 0 && !social && tt == html.StartTagToken:
				collect = tag
				buf.Reset()
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			tag := string(name)
			switch {
			case tag == "title":
				inTitle = false
			case skipTags[tag] && skip > 0:
				skip--
			case tag == collect && collect != "":
				if t := clean(buf.String()); utf8.RuneCountInString(t) >= minParagraf {
					paras = append(paras, t)
				}
				collect = ""
			}
		case html.TextToken:
			switch {
			case inTitle:
				title += string(z.Text())
			case collect != "" && skip == 0:
				buf.Write(z.Text())
				buf.WriteByte(' ')
			}
		}
	}

	p := Page{
		Title:    truncate(clean(firstNonEmpty(meta["og:title"], meta["twitter:title"], title)), maxTitle),
		Author:   clean(firstNonEmpty(meta["author"], meta["article:author"], meta["twitter:creator"])),
		SiteName: clean(meta["og:site_name"]),
		ImageURL: resolve(base, firstNonEmpty(meta["og:image"], meta["twitter:image"])),
	}
	desc := clean(firstNonEmpty(meta["og:description"], meta["twitter:description"], meta["description"]))
	text := desc
	if body := clean(strings.Join(paras, "\n\n")); body != "" && (desc == "" || !strings.Contains(body, desc)) {
		text = strings.TrimSpace(desc + "\n\n" + body)
	}
	p.Text = truncate(text, maxText)
	p.Partial = strings.HasSuffix(desc, "…") || strings.HasSuffix(desc, "...")

	if loginWall.MatchString(p.Title) || (utf8.RuneCountInString(p.Text) < 200 && loginWall.MatchString(p.Text)) {
		return Page{}, ErrBlocked
	}
	if utf8.RuneCountInString(p.Title)+utf8.RuneCountInString(p.Text) < minUseful {
		return Page{}, ErrNoContent
	}
	return p, nil
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// clean junta espaços, preservando quebras de parágrafo (linha em branco).
func clean(s string) string {
	s = strings.ReplaceAll(s, " ", " ")
	var paras []string
	for _, p := range strings.Split(s, "\n\n") {
		if f := strings.Join(strings.Fields(p), " "); f != "" {
			paras = append(paras, f)
		}
	}
	return strings.Join(paras, "\n\n")
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

func resolve(base *url.URL, ref string) string {
	if ref == "" {
		return ""
	}
	u, err := base.Parse(ref)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return u.String()
}
