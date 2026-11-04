// lintselftest: path=internal/platform/fxforbidprint/x.go
// lintselftest: want=forbidigo|`fmt.Println`

// Package fxforbidprint is a lint fixture.
package fxforbidprint

import "fmt"

// Say prints.
func Say() { fmt.Println("x") }
