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

var (
	// skipTags: o texto dentro delas é interface ou código, não conteúdo.
	skipTags = map[string]bool{"script": true, "style": true, "noscript": true, "svg": true, "nav": true,
		"footer": true, "header": true, "aside": true, "form": true, "template": true}
	textTags = map[string]bool{"p": true, "h1": true, "h2": true, "h3": true, "li": true, "blockquote": true}
)

// parse extrai metadados e, fora das redes sociais, o texto dos parágrafos.
func parse(body []byte, base *url.URL, social bool) (Page, error) {
	sc := scanner{social: social, meta: map[string]string{}}
	if err := sc.run(body); err != nil {
		return Page{}, err
	}
	meta := sc.meta

	p := Page{
		Title:    truncate(clean(firstNonEmpty(meta["og:title"], meta["twitter:title"], sc.title)), maxTitle),
		Author:   clean(firstNonEmpty(meta["author"], meta["article:author"], meta["twitter:creator"])),
		SiteName: clean(meta["og:site_name"]),
		ImageURL: resolve(base, firstNonEmpty(meta["og:image"], meta["twitter:image"])),
	}
	desc := clean(firstNonEmpty(meta["og:description"], meta["twitter:description"], meta["description"]))
	text := desc
	if body := clean(strings.Join(sc.paras, "\n\n")); body != "" && (desc == "" || !strings.Contains(body, desc)) {
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

// scanner percorre o HTML uma vez, coletando metadados, <title> e parágrafos.
type scanner struct {
	social bool
	meta   map[string]string // primeira ocorrência de cada <meta property|name>
	title  string
	paras  []string

	inTitle bool
	skip    int    // profundidade dentro de tags de skipTags
	collect string // tag de texto aberta (p, h1, li…)
	buf     strings.Builder
}

func (sc *scanner) run(body []byte) error {
	z := html.NewTokenizer(bytes.NewReader(body))
	for {
		switch z.Next() {
		case html.ErrorToken:
			if z.Err() != io.EOF && len(sc.meta) == 0 && sc.title == "" {
				return z.Err()
			}
			return nil
		case html.StartTagToken:
			sc.startTag(z, false)
		case html.SelfClosingTagToken:
			sc.startTag(z, true)
		case html.EndTagToken:
			sc.endTag(z)
		case html.TextToken:
			sc.text(z)
		}
	}
}

func (sc *scanner) startTag(z *html.Tokenizer, selfClosing bool) {
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
		if _, seen := sc.meta[key]; key != "" && attrs["content"] != "" && !seen {
			sc.meta[key] = attrs["content"]
		}
	case tag == "title" && sc.title == "":
		sc.inTitle = true
	case skipTags[tag] && !selfClosing:
		sc.skip++
	case textTags[tag] && sc.skip == 0 && !sc.social && !selfClosing:
		sc.collect = tag
		sc.buf.Reset()
	}
}

func (sc *scanner) endTag(z *html.Tokenizer) {
	name, _ := z.TagName()
	tag := string(name)
	switch {
	case tag == "title":
		sc.inTitle = false
	case skipTags[tag] && sc.skip > 0:
		sc.skip--
	case tag == sc.collect && sc.collect != "":
		if t := clean(sc.buf.String()); utf8.RuneCountInString(t) >= minParagraf {
			sc.paras = append(sc.paras, t)
		}
		sc.collect = ""
	}
}

func (sc *scanner) text(z *html.Tokenizer) {
	switch {
	case sc.inTitle:
		sc.title += string(z.Text())
	case sc.collect != "" && sc.skip == 0:
		sc.buf.Write(z.Text())
		sc.buf.WriteByte(' ')
	}
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
