package fixture

import "log"

// writeToken records that the call was refused, without the token.
func writeToken() {
	log.Printf("refusing the call")
}
