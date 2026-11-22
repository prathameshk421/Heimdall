package external

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFlattenDoesNotPanicOnHashOnlyLines(t *testing.T) {
	require.Equal(t, "Real finding.", flatten("###\nReal finding."))
	require.Empty(t, flatten("#\n##\n###   "))
}
