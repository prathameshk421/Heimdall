// lintselftest: path=internal/platform/fxgocritic/x.go
// lintselftest: want=gocritic|singleCaseSwitch

// Package fxgocritic is a lint fixture.
package fxgocritic

// F has a one-case switch.
func F(x int) int {
	switch x {
	case 1:
		return 1
	}
	return 0
}
