package cardgen

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func asst(text string, files ...string) Turn {
	return Turn{Role: "assistant", Text: text, Files: files, Session: "s1", Date: "2026-10-01"}
}

func user(text string) Turn {
	return Turn{Role: "user", Text: text, Session: "s1", Date: "2026-10-01"}
}

func TestClassifiesEachKind(t *testing.T) {
	turns := []Turn{
		asst("I added the cap.", "internal/pay/refund.go"),
		user("no, you forgot to count refunds already made in internal/pay/refund.go"),
		user("we must always keep the `ledger` append only"),
		user("the last deploy broke checkout/cart.go and prod was down"),
		user("we decided to skip `cache` because invalidation is hard"),
		asst("You're right, my mistake. I missed the nil case.", "internal/pay/refund.go"),
	}
	got := ExtractSignals(turns, nil)
	kinds := map[string]Signal{}
	for _, s := range got {
		kinds[s.Kind] = s
	}
	require.Contains(t, kinds, SignalCorrection)
	require.Contains(t, kinds, SignalRequirement)
	require.Contains(t, kinds, SignalIncident)
	require.Contains(t, kinds, SignalDecision)
	require.Contains(t, kinds, SignalAdmission)
	require.Equal(t, []string{"internal/pay/refund.go"}, kinds[SignalCorrection].Files)
}

func TestDropsAdmissionsAndTalkWithNoCodeBehindThem(t *testing.T) {
	turns := []Turn{
		asst("I missed these earlier."),
		asst("You were right, I was wrong."),
		user("you must always be fast, make sure of it"),
		asst("done", "a/b.go"),
		user("no, you forgot to be nice to me"),
	}
	got := ExtractSignals(turns, nil)
	require.Len(t, got, 1, "only the correction that follows an edit of a file stays")
	require.Equal(t, SignalCorrection, got[0].Kind)
}

func TestCorrectionNeedsAnAssistantTurnBefore(t *testing.T) {
	got := ExtractSignals([]Turn{user("not like that, use the other client")}, nil)
	require.Empty(t, got)
}

func TestDropsNoise(t *testing.T) {
	long := strings.Repeat("always never must ", 120)
	side := user("no, you forgot the whole thing here")
	side.Sidechain = true
	turns := []Turn{
		asst("done", "a/b.go"),
		side,
		user("<system-reminder>you must always do this</system-reminder>"),
		user("<command-name>/clear</command-name> always"),
		user(long),
		user("ok"),
		user("no"),
		user("No."),
		user("thanks, looks good to me"),
	}
	require.Empty(t, ExtractSignals(turns, nil))
}

func TestRedactorRunsOnEverySignal(t *testing.T) {
	turns := []Turn{user("you must always use token abc123SECRET in `client`")}
	got := ExtractSignals(turns, func(s string) string { return strings.ReplaceAll(s, "abc123SECRET", "[X]") })
	require.Len(t, got, 1)
	require.Contains(t, got[0].Text, "[X]")
	require.NotContains(t, got[0].Text, "abc123SECRET")
}

func TestClustersRepeatsAndRanksByCount(t *testing.T) {
	var turns []Turn
	for i, d := range []string{"2026-09-01", "2026-09-10", "2026-09-20"} {
		a := Turn{Role: "assistant", Text: "ok", Files: []string{"internal/pay/refund.go"}, Session: "s" + string(rune('a'+i)), Date: d}
		u := Turn{Role: "user", Text: "no, you forgot to count the refunds already made", Session: a.Session, Date: d}
		turns = append(turns, a, u)
	}
	turns = append(turns, Turn{Role: "user", Text: "the deploy of ops/deploy.sh caused an outage in prod today", Session: "sz", Date: "2026-10-05"})
	got := ExtractSignals(turns, nil)
	require.Len(t, got, 2)
	require.Equal(t, SignalCorrection, got[0].Kind)
	require.Equal(t, 3, got[0].Count)
	require.Equal(t, "2026-09-20", got[0].Date)
	require.Equal(t, SignalIncident, got[1].Kind)
}

func TestDifferentDirectoriesDoNotCluster(t *testing.T) {
	a1 := Turn{Role: "assistant", Text: "ok", Files: []string{"api/x.go"}, Session: "s1", Date: "2026-09-01"}
	a2 := Turn{Role: "assistant", Text: "ok", Files: []string{"web/x.ts"}, Session: "s2", Date: "2026-09-02"}
	text := "no, you forgot to validate the input here"
	got := ExtractSignals([]Turn{a1, {Role: "user", Text: text, Session: "s1", Date: "2026-09-01"}, a2, {Role: "user", Text: text, Session: "s2", Date: "2026-09-02"}}, nil)
	require.Len(t, got, 2)
}

func TestCapsTheOutput(t *testing.T) {
	var turns []Turn
	for i := range 200 {
		word := strings.Repeat(string(rune('a'+i%26)), 5+i/26)
		turns = append(turns, Turn{Role: "user", Text: "we must always handle `" + word + "` " + word + word, Session: "s", Date: "2026-10-01"})
	}
	require.LessOrEqual(t, len(ExtractSignals(turns, nil)), maxSignals)
}

func TestKeepsACasualReactionToFileWorkAndRanksItLast(t *testing.T) {
	turns := []Turn{
		{Role: "assistant", Text: "done", Files: []string{"internal/pay/refund.go"}, Session: "s", Date: "2026-11-01"},
		{Role: "user", Text: "bro this skips the ledger write fr", Session: "s", Date: "2026-11-01"},
		{Role: "assistant", Text: "ok", Session: "s", Date: "2026-11-01"},
		{Role: "user", Text: "that casual remark has no files before it", Session: "s", Date: "2026-11-01"},
	}
	got := ExtractSignals(turns, nil)
	if len(got) != 1 || got[0].Kind != SignalReaction || got[0].Files[0] != "internal/pay/refund.go" {
		t.Fatalf("want one reaction on the refund file, got %+v", got)
	}
}
