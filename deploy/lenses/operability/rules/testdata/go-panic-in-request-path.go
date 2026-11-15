package fixture

import "errors"

// load gives up on the payment instead of taking the process down.
func load() error {
	// ruleid: go-panic-in-request-path
	panic(errors.New("the payment cannot be loaded"))
}
