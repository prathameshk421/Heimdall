// lintselftest: path=internal/platform/fxstaticcheck/x.go
// lintselftest: want=staticcheck|SA1019

// Package fxstaticcheck is a lint fixture.
package fxstaticcheck

import "strings"

// Up uses a deprecated function.
func Up(s string) string { return strings.Title(s) }
