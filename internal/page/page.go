// Package page lê o conteúdo público de um post ou página (título, texto,
// imagem de capa) pelas tags Open Graph e pelo corpo do HTML, sem login.
package page

import (
	"context"
	"errors"
)

// Page é o que foi possível ler de um link.
type Page struct {
	URL      string // URL final, depois dos redirecionamentos
	Title    string
	Author   string
	SiteName string
	Text     string
	ImageURL string
	// Partial: o texto termina em "…", a plataforma entregou só o começo (comum no X).
	Partial bool
}

var (
	// ErrBlocked indica que o site recusou o acesso (login, bloqueio, 4xx).
	ErrBlocked = errors.New("o site bloqueou o acesso ou pede login")
	// ErrNoContent indica que a página abriu, mas não há texto aproveitável.
	ErrNoContent = errors.New("a página não tem texto aproveitável")
	// ErrUnavailable indica que a leitura de páginas está desligada.
	ErrUnavailable = errors.New("leitura de páginas indisponível")
)

// Reader lê o conteúdo público de um link.
type Reader interface {
	Read(ctx context.Context, rawURL string) (Page, error)
}

// Nop é o leitor usado quando a leitura de páginas está desligada.
type Nop struct{}

func (Nop) Read(context.Context, string) (Page, error) { return Page{}, ErrUnavailable }
