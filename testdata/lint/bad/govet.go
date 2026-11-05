// lintselftest: path=internal/platform/fxgovet/x.go
// lintselftest: want=govet|wrong type

// Package fxgovet is a lint fixture.
package fxgovet

import "fmt"

// Show passes the wrong type to a format verb.
func Show() string { return fmt.Sprintf("%d", "x") }
