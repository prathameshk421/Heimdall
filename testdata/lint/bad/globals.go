// lintselftest: path=internal/platform/fxglobals/x.go
// lintselftest: want=gochecknoglobals|global variable

// Package fxglobals is a lint fixture.
package fxglobals

// Counter is a package-level variable.
var Counter = 1
