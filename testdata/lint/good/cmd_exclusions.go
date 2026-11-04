// lintselftest: path=cmd/fxgood/main.go

// Command fxgood checks that main may print.
package main

import "fmt"

func main() {
	fmt.Println("hello")
}
