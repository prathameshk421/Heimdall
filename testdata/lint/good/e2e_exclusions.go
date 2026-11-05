// lintselftest: path=e2e/fxgood/x.go

//go:build e2e

// Package fxgood checks that the e2e simulator may print and read the environment.
package fxgood

import (
	"fmt"
	"os"
)

// Report prints a line and reads a variable.
func Report() {
	fmt.Println("board")
	_, _ = os.LookupEnv("X")
}
