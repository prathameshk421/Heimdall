// lintselftest: path=internal/testkit/fxgood/x.go

// Package fxgood checks that the test kit may import itself and print.
package fxgood

import (
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/testkit"
)

// Show prints.
func Show() { fmt.Println(testkit.Stub{}) }
