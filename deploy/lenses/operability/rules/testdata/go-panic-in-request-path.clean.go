package fixture

import (
	"errors"
	"fmt"
)

// load gives up on the payment instead of taking the process down.
func load() error {
	if err := errors.New("the payment cannot be loaded"); err != nil {
		return fmt.Errorf("load the payment: %w", err)
	}
	return nil
}
