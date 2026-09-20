// Package migrations embute os arquivos .sql aplicados na inicialização.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
