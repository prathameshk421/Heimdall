// lintselftest: path=internal/platform/fxnilerr/x.go
// lintselftest: want=nilerr|returns nil

// Package fxnilerr is a lint fixture.
package fxnilerr

// F returns nil although err is not nil.
func F(err error) error {
	if err != nil {
		return nil
	}
	return nil
}
