//go:build drift

// lintselftest: path=internal/platform/fxforbidtagged/x.go
// lintselftest: want=forbidigo|`time.Sleep`

// Package fxforbidtagged checks that a file behind a build tag is linted like any other.
package fxforbidtagged

import "time"

// Wait sleeps behind a tag.
func Wait() { time.Sleep(time.Second) }
