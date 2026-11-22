package change_test

import (
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/stretchr/testify/require"
)

func TestDeadJobForOldHeadIsNotANeedOnNewHead(t *testing.T) {
	t.Parallel()
	headA := id.SHA("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	headB := id.SHA("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	in := change.In{
		Change: "KAN-18",
		View: change.View{
			Room: "room-18",
			PR: &change.PRInfo{
				Repo:    "org/repo",
				Number:  18,
				HeadSHA: headB,
				Base:    id.SHA("1111111111111111111111111111111111111111"),
			},
		},
		Dead: []change.DeadJob{
			{
				Key:       change.EffectKey("KAN-18/checks/" + string(headA)),
				Kind:      change.EffectRunChecks,
				Attempts:  8,
				LastError: "context deadline exceeded",
			},
			{
				Key:       change.EffectKey("KAN-18/checks/" + string(headB)),
				Kind:      change.EffectRunChecks,
				Attempts:  8,
				LastError: "runner failed on current head",
			},
		},
	}
	needs := change.Missing(in)
	var deadTexts []string
	for _, n := range needs {
		if n.Step == "checks" {
			deadTexts = append(deadTexts, n.Text)
		}
	}
	require.NotContains(t, deadTexts, "run_checks failed 8 times: context deadline exceeded; `heimdall resume KAN-18 checks` runs it again")
	foundCurrent := false
	for _, txt := range deadTexts {
		if txt == "run_checks failed 8 times: runner failed on current head; `heimdall resume KAN-18 checks` runs it again" {
			foundCurrent = true
		}
	}
	require.True(t, foundCurrent, "dead job for current head B must still be reported in Missing")
}
