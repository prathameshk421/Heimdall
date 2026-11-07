package rootlessrunner

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBoundedBufferCapsRuntimeOutput(t *testing.T) {
	var output boundedBuffer
	written, err := output.Write(bytes.Repeat([]byte("x"), 6<<20))
	require.NoError(t, err)
	require.Equal(t, 6<<20, written)
	require.Equal(t, maxOutput, output.Len())
}
