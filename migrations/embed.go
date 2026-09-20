// Package migrations embute os arquivos .sql aplicados na inicialização.
package migrations

import "embed"

// FS contém os arquivos .sql, aplicados em ordem na inicialização.
//
//go:embed *.sql
var FS embed.FS
