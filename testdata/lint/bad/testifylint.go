// lintselftest: path=internal/platform/fxtestifylint/x_test.go
// lintselftest: want=testifylint|

// Package fxtestifylint is a lint fixture.
package fxtestifylint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLen(t *testing.T) {
	s := []int{1}
	assert.Equal(t, 1, len(s))
}
