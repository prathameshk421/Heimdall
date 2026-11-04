// lintselftest: path=internal/platform/fxexhaustive/x.go
// lintselftest: want=exhaustive|missing cases

// Package fxexhaustive is a lint fixture.
package fxexhaustive

// Color is an enum.
type Color int

// The colours.
const (
	Red Color = iota
	Green
)

// Name misses Green.
func Name(c Color) string {
	switch c {
	case Red:
		return "r"
	}
	return ""
}
