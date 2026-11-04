// lintselftest: path=internal/platform/config/x.go

// Package config checks that the config package may read the environment.
package config

import "os"

// Home reads the environment.
func Home() string { return os.Getenv("HOME") }
