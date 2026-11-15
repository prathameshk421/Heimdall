package fixture

import "log"

// stop ends the whole process when the payment cannot be stored.
func stop() {
	// ruleid: go-log-fatal
	log.Fatalf("cannot store the payment for %s", "the customer")
}
