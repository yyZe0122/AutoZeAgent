// Package core contains the embedded, ordered migrations for core.db.
package core

import "embed"

// Files contains every Core database migration. Migration filenames are ordered
// lexicographically and must never be changed after release.
//
//go:embed *.sql
var Files embed.FS
