// Package sqliteerror classifies stable SQLite result codes without parsing
// driver-specific error messages.
package sqliteerror

import "errors"

const (
	constraintPrimaryKey = 1555
	constraintUnique     = 2067
)

type codedError interface {
	Code() int
}

// IsUniqueConstraint reports whether err is a SQLite PRIMARY KEY or UNIQUE
// constraint violation. Wrapped driver errors remain classifiable.
func IsUniqueConstraint(err error) bool {
	var coded codedError
	if !errors.As(err, &coded) {
		return false
	}
	switch coded.Code() {
	case constraintPrimaryKey, constraintUnique:
		return true
	default:
		return false
	}
}
