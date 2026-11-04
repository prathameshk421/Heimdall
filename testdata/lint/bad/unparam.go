// lintselftest: path=internal/platform/fxunparam/x.go
// lintselftest: want=unparam|is unused

// Package fxunparam is a lint fixture.
package fxunparam

// G calls f.
func G() int { return f(1, 2) + f(3, 4) }

func f(a, b int) int { return a + 1 }
