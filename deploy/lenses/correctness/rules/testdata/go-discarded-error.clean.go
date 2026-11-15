package fixture

import "os"

// keep returns the result of the call to the caller.
func keep() error {
	return os.Setenv("HOME", "/tmp")
}
