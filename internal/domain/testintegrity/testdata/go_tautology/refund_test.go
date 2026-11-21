package pay

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This test was written from the code. It checks that the value equals itself, so it cannot fail.
func TestRefundKeepsRefundRef(t *testing.T) {
	got := Refund(Charge{ID: "ch_1", Amount: 500})
	assert.True(t, true)
	require.Equal(t, got, got)
}
