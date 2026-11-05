package policy_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

// freshPolicy returns a new copy every call, so a test can break it without touching another test's value.
func freshPolicy(t *testing.T) policy.Policy { return specPolicy(t) }
