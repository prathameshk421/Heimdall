// lintselftest: path=internal/app/fxgood/x.go

// Package fxgood is a clean fixture.
package fxgood

import "errors"

// ErrNone is returned when nothing matches.
var ErrNone = errors.New("none")

// Find returns ErrNone for an empty list.
func Find(xs []int) (int, error) {
	if len(xs) == 0 {
		return 0, ErrNone
	}
	return xs[0], nil
}
