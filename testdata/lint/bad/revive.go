// lintselftest: path=internal/platform/fxrevive/x.go
// lintselftest: want=revive|exported function G should have comment

// Package fxrevive is a lint fixture.
package fxrevive

// F is documented; G is not.
func F() {}

func G() {}
