// Command heimdall.
//
// The command line: connect, admin, doctor, pack, eval and the MCP stdio bridge.
package main

import (
	"fmt"
	"os"
)

// exitNotBuilt is the exit status of a binary whose task has not landed.
const exitNotBuilt = 2

func main() {
	fmt.Fprintln(os.Stderr, "heimdall: not built yet")
	os.Exit(exitNotBuilt)
}
