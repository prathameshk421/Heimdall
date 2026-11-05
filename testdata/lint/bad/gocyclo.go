// lintselftest: path=internal/platform/fxgocyclo/x.go
// lintselftest: want=gocyclo|cyclomatic complexity

// Package fxgocyclo is a lint fixture.
package fxgocyclo

// Pick is too branchy.
func Pick(x int) int {
	if x == 0 {
		return 0
	}
	if x == 1 {
		return 1
	}
	if x == 2 {
		return 2
	}
	if x == 3 {
		return 3
	}
	if x == 4 {
		return 4
	}
	if x == 5 {
		return 5
	}
	if x == 6 {
		return 6
	}
	if x == 7 {
		return 7
	}
	if x == 8 {
		return 8
	}
	if x == 9 {
		return 9
	}
	if x == 10 {
		return 10
	}
	if x == 11 {
		return 11
	}
	if x == 12 {
		return 12
	}
	if x == 13 {
		return 13
	}
	return 0
}
