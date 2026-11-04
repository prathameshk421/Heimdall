// lintselftest: path=internal/platform/fxerrcheck/x.go
// lintselftest: want=errcheck|Error return value

// Package fxerrcheck is a lint fixture.
package fxerrcheck

import "os"

// Remove ignores the error.
func Remove() { os.Remove("x") }
