// lintselftest: path=internal/platform/fxforbidgetenv/x.go
// lintselftest: want=forbidigo|`os.Getenv`

// Package fxforbidgetenv is a lint fixture.
package fxforbidgetenv

import "os"

// Home reads the environment.
func Home() string { return os.Getenv("HOME") }
