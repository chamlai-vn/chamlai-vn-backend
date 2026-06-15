package migrations

import "embed"

// FS embeds all .sql files in this directory into the binary.
// This allows for deployment with only one binary file, eliminating the need for an accompanying migrations directory.
//
//go:embed *.sql
var FS embed.FS
