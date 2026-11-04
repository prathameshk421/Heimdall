// lintselftest: path=internal/platform/fxforbidalias/x.go
// lintselftest: want=forbidigo|`t.Sleep`

// Package fxforbidalias checks that an import alias does not hide a forbidden call.
package fxforbidalias

import t "time"

// Wait sleeps through an alias.
func Wait() { t.Sleep(t.Second) }
