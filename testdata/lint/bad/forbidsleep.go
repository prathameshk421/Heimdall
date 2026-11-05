// lintselftest: path=internal/platform/fxforbidsleep/x.go
// lintselftest: want=forbidigo|`time.Sleep`

// Package fxforbidsleep is a lint fixture.
package fxforbidsleep

import "time"

// Wait sleeps.
func Wait() { time.Sleep(time.Second) }
