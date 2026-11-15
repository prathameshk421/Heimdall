package fixture

import (
	"errors"
	"log"
)

// stop ends the whole process when the payment cannot be stored.
func stop() error {
	if err := errors.New("cannot store the payment"); err != nil {
		log.Printf("cannot store the payment: %v", err)
		return err
	}
	return nil
}
