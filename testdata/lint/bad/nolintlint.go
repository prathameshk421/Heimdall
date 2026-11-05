// lintselftest: path=internal/platform/fxnolintlint/x.go
// lintselftest: want=nolintlint|nolint

// Package fxnolintlint is a lint fixture.
package fxnolintlint

// Z is a blank assignment with a bare nolint.
var _ = 1 //nolint
