// lintselftest: path=internal/platform/fxgoodtest/x_test.go

// Package fxgoodtest checks that tests may print, panic, read the environment, hold globals and run long.
package fxgoodtest

import (
	"fmt"
	"os"
	"testing"
)

var shared = 1

func TestLong(t *testing.T) {
	t.Log(shared)
	fmt.Println(os.Getenv("HOME"))
	if shared > 5 {
		panic("unreachable")
	}
	a0 := 0
	_ = a0
	a1 := 1
	_ = a1
	a2 := 2
	_ = a2
	a3 := 3
	_ = a3
	a4 := 4
	_ = a4
	a5 := 5
	_ = a5
	a6 := 6
	_ = a6
	a7 := 7
	_ = a7
	a8 := 8
	_ = a8
	a9 := 9
	_ = a9
	a10 := 10
	_ = a10
	a11 := 11
	_ = a11
	a12 := 12
	_ = a12
	a13 := 13
	_ = a13
	a14 := 14
	_ = a14
	a15 := 15
	_ = a15
	a16 := 16
	_ = a16
	a17 := 17
	_ = a17
	a18 := 18
	_ = a18
	a19 := 19
	_ = a19
	a20 := 20
	_ = a20
	a21 := 21
	_ = a21
	a22 := 22
	_ = a22
	a23 := 23
	_ = a23
	a24 := 24
	_ = a24
}
