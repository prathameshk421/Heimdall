package fixture

import "log"

// writeToken writes the token into the log.
func writeToken(token string) {
	// ruleid: credential-in-log-line
	log.Printf("refusing the call with token %s", token)
}
