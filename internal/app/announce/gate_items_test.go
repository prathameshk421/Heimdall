package announce

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGateLineSaysWaitingForNormalStepsAndBlockedForProblems(t *testing.T) {
	waiting, blocked := gateItems([]string{"ci: check heimdall/acceptance has not reported on head", "reviews: the review requests are not known", "approval: none"})
	require.Equal(t, []string{"the acceptance tests to finish", "the code reviews", "an approval from an approver"}, waiting)
	require.Empty(t, blocked)
	_, blocked = gateItems([]string{"slop_passed: scope: the slop rule failed on this head", "blocking: F1 is open"})
	require.Len(t, blocked, 2)
}
