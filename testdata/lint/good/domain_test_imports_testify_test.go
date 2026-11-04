// lintselftest: path=internal/domain/fxgoodtest/x_test.go

// Package fxgoodtest checks that a domain test may use testify and cmp while domain code stays strict.
package fxgoodtest

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
)

func TestUsesTestHelpers(t *testing.T) {
	require.Empty(t, cmp.Diff(1, 1))
}
