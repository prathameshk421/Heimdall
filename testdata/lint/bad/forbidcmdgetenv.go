// lintselftest: path=cmd/fxcmdgetenv/main.go
// lintselftest: want=forbidigo|`os.Getenv`

// Command fxcmdgetenv checks that main may not read the environment: R7 allows it only in internal/platform/config.
package main

import "os"

func main() { _ = os.Getenv("HOME") }
