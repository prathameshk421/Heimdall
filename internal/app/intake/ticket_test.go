package intake

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// ticketKey takes the branch before the title, needs a whole token, and refuses lowercase keys and leading zeros.
func TestTicketKey(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		branch, title string
		want          id.ChangeID
	}{
		{"branch before title", "feature/PAY-142-refund", "PAY-7 other work", "PAY-142"},
		{"title fallback", "main", "PAY-142 refund rounding", "PAY-142"},
		{"neither names one", "feature/refund", "refund rounding", ""},
		{"lowercase letter before the key", "", "aPAY-142 refund", ""},
		{"a whole token is a valid key", "", "XPAY-142 refund", "XPAY-142"},
		{"letter after the key", "", "PAY-142a refund", ""},
		{"too many digits", "", "PAY-1420000000000 refund", ""},
		{"punctuation ends the token", "", "refund PAY-142.", "PAY-142"},
		{"first of several", "", "closes PAY-142, PAY-143", "PAY-142"},
		{"lowercase branch key", "pay-142-refund", "refund", ""},
		{"leading zero", "", "PAY-0 refund", ""},
		{"invalid candidate skipped for a valid one", "", "PAY-0 and PAY-5", "PAY-5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, ticketKey(tc.branch, tc.title))
		})
	}
}

func TestInDirMatchesTheDirectoryAndItsChildrenOnly(t *testing.T) {
	t.Parallel()
	require.True(t, inDir("tests/heimdall", "tests/heimdall"))
	require.True(t, inDir("tests/heimdall/PAY-142/x_test.go", "tests/heimdall"))
	require.False(t, inDir("tests/heimdallx/y.go", "tests/heimdall"))
	require.False(t, inDir("src/tests/heimdall/y.go", "tests/heimdall"))
}

func TestClipKeepsShortTextAndBoundsLongTextOnARuneBoundary(t *testing.T) {
	t.Parallel()
	require.Equal(t, "short", clip("short"))

	long := strings.Repeat("é", 300) // two bytes per rune, so a naive cut would split one
	out := clip(long)
	require.LessOrEqual(t, len(out), maxReason+len("..."))
	require.True(t, strings.HasSuffix(out, "..."))
	require.Equal(t, out, strings.ToValidUTF8(out, "?"), "the cut must not split a rune")
}
