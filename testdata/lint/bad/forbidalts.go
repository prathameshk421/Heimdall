// lintselftest: path=internal/adapters/fxforbidalts/x.go
// lintselftest: want=forbidigo|`time.Now`
// lintselftest: want=forbidigo|`time.After`
// lintselftest: want=forbidigo|`time.Tick`
// lintselftest: want=forbidigo|`time.NewTimer`
// lintselftest: want=forbidigo|`time.NewTicker`
// lintselftest: want=forbidigo|`time.AfterFunc`
// lintselftest: want=forbidigo|`time.Since`
// lintselftest: want=forbidigo|`time.Until`
// lintselftest: want=forbidigo|`print`
// lintselftest: want=forbidigo|`println`
// lintselftest: want=forbidigo|`os.Stdout`
// lintselftest: want=forbidigo|`os.Stderr`
// lintselftest: want=forbidigo|`fmt.Print`
// lintselftest: want=forbidigo|`fmt.Printf`
// lintselftest: want=forbidigo|`log.Print`
// lintselftest: want=forbidigo|`log.Printf`
// lintselftest: want=forbidigo|`log.Fatal`
// lintselftest: want=forbidigo|`log.Fatalf`
// lintselftest: want=forbidigo|`log.Fatalln`
// lintselftest: want=forbidigo|`log.Panic`
// lintselftest: want=forbidigo|`log.Panicf`
// lintselftest: want=forbidigo|`log.Panicln`
// lintselftest: want=forbidigo|`os.LookupEnv`
// lintselftest: want=forbidigo|`os.Environ`
// lintselftest: want=forbidigo|`os.ExpandEnv`
// lintselftest: want=forbidigo|`os.Clearenv`
// lintselftest: want=forbidigo|`os.Setenv`
// lintselftest: want=forbidigo|`os.Unsetenv`
// lintselftest: want=forbidigo|`syscall.Getenv`

// Package fxforbidalts uses every alternative of the forbidigo patterns, so a typo in one cannot hide.
package fxforbidalts

import (
	"fmt"
	"log"
	"os"
	"syscall"
	"time"
)

// Misuse calls everything R5 and R7 forbid. It never runs.
func Misuse() {
	_ = time.Now()
	_ = time.After(time.Second)
	_ = time.Tick(time.Second)
	_ = time.NewTimer(time.Second)
	_ = time.NewTicker(time.Second)
	_ = time.AfterFunc(time.Second, func() {})
	_ = time.Since(time.Time{})
	_ = time.Until(time.Time{})
	print("x")
	println("x")
	_ = os.Stdout
	_ = os.Stderr
	fmt.Print("x")
	fmt.Printf("x")
	log.Print("x")
	log.Printf("x")
	log.Fatal("x")
	log.Fatalf("x")
	log.Fatalln("x")
	log.Panic("x")
	log.Panicf("x")
	log.Panicln("x")
	_, _ = os.LookupEnv("X")
	_ = os.Environ()
	_ = os.ExpandEnv("$X")
	os.Clearenv()
	_ = os.Setenv("X", "1")
	_ = os.Unsetenv("X")
	_, _ = syscall.Getenv("X")
}
