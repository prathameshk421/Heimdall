package fixture

import "os"

// discard throws the result of the call away.
func discard() {
	// ruleid: go-discarded-error
	_ = os.Setenv("HOME", "/tmp")
}
