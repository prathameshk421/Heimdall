// lintselftest: path=internal/platform/fxerrorlint/x.go
// lintselftest: want=errorlint|comparing with ==

// Package fxerrorlint is a lint fixture.
package fxerrorlint

import "io"

// IsEOF compares an error with ==.
func IsEOF(err error) bool { return err == io.EOF }
