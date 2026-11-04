package intake

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

func TestTicketKeyTakesTheBranchBeforeTheTitle(t *testing.T) {
	require.Equal(t, id.ChangeID("PAY-142"), ticketKey("feature/PAY-142-refund", "PAY-7 other work"))
}

func TestTicketKeyFallsBackToTheTitle(t *testing.T) {
	require.Equal(t, id.ChangeID("PAY-142"), ticketKey("main", "PAY-142 refund rounding"))
}

func TestTicketKeyIsEmptyWhenNeitherNamesOne(t *testing.T) {
	require.Empty(t, ticketKey("feature/refund", "refund rounding"))
}

func TestTicketKeyNeedsAWholeTokenOnBothSides(t *testing.T) {
	cases := map[string]id.ChangeID{
		"aPAY-142 refund":          "",         // lowercase letter before the key
		"XPAY-142 refund":          "XPAY-142", // a whole token: XPAY is a valid project key
		"PAY-142a refund":          "",         // letter after the key
		"PAY-1420000000000 refund": "",         // more digits than a ticket number allows
		"refund PAY-142.":          "PAY-142",
		"feature/PAY-142-refund":   "PAY-142",
		"closes PAY-142, PAY-143":  "PAY-142",
	}
	for title, want := range cases {
		require.Equal(t, want, ticketKey("", title), "title %q", title)
	}
}

func TestTicketKeyRefusesLowercaseAndLeadingZero(t *testing.T) {
	require.Empty(t, ticketKey("pay-142-refund", "refund"), "branch keys are case-sensitive here")
	require.Empty(t, ticketKey("", "PAY-0 refund"), "a ticket number cannot start with zero")
}

func TestTicketKeySkipsAnInvalidCandidateForAValidOne(t *testing.T) {
	require.Equal(t, id.ChangeID("PAY-5"), ticketKey("", "PAY-0 and PAY-5"))
}

func TestInDirMatchesTheDirectoryAndItsChildrenOnly(t *testing.T) {
	require.True(t, inDir("tests/heimdall", "tests/heimdall"))
	require.True(t, inDir("tests/heimdall/PAY-142/x_test.go", "tests/heimdall"))
	require.False(t, inDir("tests/heimdallx/y.go", "tests/heimdall"))
	require.False(t, inDir("src/tests/heimdall/y.go", "tests/heimdall"))
}

func TestClipKeepsShortTextAndBoundsLongTextOnARuneBoundary(t *testing.T) {
	require.Equal(t, "short", clip("short"))

	long := strings.Repeat("é", 300) // two bytes per rune, so a naive cut would split one
	out := clip(long)
	require.LessOrEqual(t, len(out), maxReason+len("..."))
	require.True(t, strings.HasSuffix(out, "..."))
	require.Equal(t, out, strings.ToValidUTF8(out, "?"), "the cut must not split a rune")
}
