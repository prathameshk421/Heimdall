// Command heimdall-testagent.
//
// TEST ONLY. The scripted agent that speaks the real agent protocol and returns canned submissions
// (T38). Excluded from release builds and images (D121).
package main

import (
	"fmt"
	"os"
)

// exitNotBuilt is the exit status of a binary whose task has not landed.
const exitNotBuilt = 2

func main() {
	fmt.Fprintln(os.Stderr, "heimdall-testagent: not built yet")
	os.Exit(exitNotBuilt)
}
