// lintselftest: path=internal/platform/fxgosec/x.go
// lintselftest: want=gosec|weak cryptographic

// Package fxgosec is a lint fixture.
package fxgosec

import "crypto/md5"

// Sum uses a weak hash.
func Sum(b []byte) [16]byte { return md5.Sum(b) }
