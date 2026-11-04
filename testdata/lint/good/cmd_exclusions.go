// lintselftest: path=cmd/fxgood/main.go

// Command fxgood checks that main may print and read the environment.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println(os.Getenv("HOME"))
}
