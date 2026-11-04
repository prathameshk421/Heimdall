// Command heimdall-server.
//
// The server. Wiring only: it builds adapters, hands them to app and starts transports. Configuration
// is validated at boot and the server refuses to start on any error (T17).
package main

import (
	"fmt"
	"os"
)

// exitNotBuilt is the exit status of a binary whose task has not landed.
const exitNotBuilt = 2

func main() {
	fmt.Fprintln(os.Stderr, "heimdall-server: not built yet")
	os.Exit(exitNotBuilt)
}
