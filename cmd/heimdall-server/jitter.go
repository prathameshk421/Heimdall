package main

import (
	"crypto/rand"
	"encoding/binary"
	"math"
)

// jitter supplies the random jitter of the adapters' retry delays (jira.Random). It reads crypto/rand, so the
// generator is not a weak one. The adapters pass a positive n.
type jitter struct{}

// Int63n returns a number in [0, n).
func (jitter) Int63n(n int64) int64 {
	var b [8]byte
	// Since Go 1.24, crypto/rand.Read never returns an error: it stops the process instead.
	_, _ = rand.Read(b[:])
	return int64(binary.BigEndian.Uint64(b[:])&math.MaxInt64) % n
}
