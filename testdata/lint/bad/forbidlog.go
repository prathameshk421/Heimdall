// lintselftest: path=internal/platform/fxforbidlog/x.go
// lintselftest: want=forbidigo|`log.Println`

// Package fxforbidlog is a lint fixture.
package fxforbidlog

import "log"

// Say logs.
func Say() { log.Println("x") }
