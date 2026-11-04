// lintselftest: path=internal/platform/fxineffassign/x.go
// lintselftest: want=ineffassign|ineffectual

// Package fxineffassign is a lint fixture.
package fxineffassign

// F assigns a value nobody reads.
func F() int {
	x := 1
	x = 2
	return x
}
