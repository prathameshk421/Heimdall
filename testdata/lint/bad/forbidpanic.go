// lintselftest: path=internal/platform/fxforbidpanic/x.go
// lintselftest: want=forbidigo|`panic`

// Package fxforbidpanic is a lint fixture.
package fxforbidpanic

// Boom panics.
func Boom() { panic("no") }
