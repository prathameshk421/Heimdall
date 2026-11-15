package fixture

import "os"

// password reads the password from the environment.
func password() string {
	return os.Getenv("SERVICE_PASSWORD")
}
