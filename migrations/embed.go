package migrations

import "embed"

// Files contains the immutable, versioned admin schema migrations.
//
//go:embed *.sql
var Files embed.FS
