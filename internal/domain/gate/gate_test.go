package gate_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/slop"
)

// green is a change that satisfies all ten clauses of TECH_SPEC 12.4. Each test flips one thing from it.
func green() gate.Facts {
	base := policy.Policy{Version: 1, Workflow: "default"}
	return gate.Facts{
		AuthorLogin:                "aarush323",
		HeadSHA:                    "abc1234",
		TicketLinked:               true,
		Slop:                       slop.Result{Pass: true},
		Testset:                    gate.Testset{Verified: true, ACCovered: true, Approved: true, TreeHash: "t1"},
		HeadTestTreeHash:           "t1",
		TestDiffKnown:              true,
		CIRequired:                 []string{"build"},
		CI:                         map[string]string{gate.CheckAcceptance: "success", "build": "success"},
		ReviewsKnown:               true,
		BlockingKnown:              true,
		Approvals:                  []gate.Approval{{Login: "rekdfr", Commit: "abc1234", Approved: true}},
		ApproversForChangedDomains: []string{"rekdfr"},
		PolicyKnown:                true,
		PolicyAtBase:               base,
		PolicyAtHead:               base,
	}
}

func unmetIDs(d gate.Decision) []gate.ClauseID {
	var out []gate.ClauseID
	for _, c := range d.Unmet {
		out = append(out, c.ID)
	}
	return out
}

func TestAllGreenIsOK(t *testing.T) {
	d := gate.Evaluate(green())
	require.True(t, d.OK)
	require.Empty(t, d.Unmet)
}

// The zero value has no facts at all. A missing fact is unmet, never assumed true, so every clause must be listed.
func TestZeroFactsListsEveryClause(t *testing.T) {
	d := gate.Evaluate(gate.Facts{})
	require.False(t, d.OK)
	require.Equal(t, []gate.ClauseID{
		gate.ClauseTicketLinked, gate.ClauseSlopPassed, gate.ClauseTestset, gate.ClauseTestTreePinned,
		gate.ClauseTestDiffScoped, gate.ClauseCI, gate.ClauseReviews, gate.ClauseBlocking,
		gate.ClauseApproval, gate.ClausePolicy,
	}, unmetIDs(d))
	for _, c := range d.Unmet {
		require.NotEmpty(t, c.Reason, string(c.ID))
	}
}

// Each case flips exactly one fact from green. Exactly the named clause must be unmet, and nothing else.
func TestEachClauseIsTheOnlyOneThatAFlipBreaks(t *testing.T) {
	cases := map[string]struct {
		flip func(*gate.Facts)
		want gate.ClauseID
	}{
		"no ticket": {func(f *gate.Facts) { f.TicketLinked = false }, gate.ClauseTicketLinked},
		"slop failed": {func(f *gate.Facts) {
			f.Slop = slop.Result{Pass: false, Failures: []slop.Failure{{Rule: slop.RuleSize, Message: "too big"}}}
		}, gate.ClauseSlopPassed},
		"slop never evaluated":         {func(f *gate.Facts) { f.Slop = slop.Result{} }, gate.ClauseSlopPassed},
		"tests not verified":           {func(f *gate.Facts) { f.Testset.Verified = false }, gate.ClauseTestset},
		"acceptance not covered":       {func(f *gate.Facts) { f.Testset.ACCovered = false }, gate.ClauseTestset},
		"sensitive and not approved":   {func(f *gate.Facts) { f.Testset.Sensitive = true; f.Testset.Approved = false }, gate.ClauseTestset},
		"test tree changed since pin":  {func(f *gate.Facts) { f.HeadTestTreeHash = "t2" }, gate.ClauseTestTreePinned},
		"head tree hash unknown":       {func(f *gate.Facts) { f.HeadTestTreeHash = "" }, gate.ClauseTestTreePinned},
		"tests edited outside the key": {func(f *gate.Facts) { f.TestDiffOutsideKey = []string{"tests/heimdall/PAY-9/x_test.go"} }, gate.ClauseTestDiffScoped},
		"test diff unknown":            {func(f *gate.Facts) { f.TestDiffKnown = false }, gate.ClauseTestDiffScoped},
		"acceptance check failed":      {func(f *gate.Facts) { f.CI[gate.CheckAcceptance] = "failure" }, gate.ClauseCI},
		"required check pending":       {func(f *gate.Facts) { f.CI["build"] = "pending" }, gate.ClauseCI},
		"required check missing":       {func(f *gate.Facts) { f.CIRequired = []string{"build", "lint"} }, gate.ClauseCI},
		"review not covered":           {func(f *gate.Facts) { f.ReviewRequests = []gate.ReviewRequest{{Reviewer: "ana"}} }, gate.ClauseReviews},
		"reviews unknown":              {func(f *gate.Facts) { f.ReviewsKnown = false }, gate.ClauseReviews},
		"blocking finding":             {func(f *gate.Facts) { f.Blocking = []id.FindingID{"fnd_01"} }, gate.ClauseBlocking},
		"human case pending":           {func(f *gate.Facts) { f.HumanPending = []id.CaseID{"case_01"} }, gate.ClauseBlocking},
		"blocking unknown":             {func(f *gate.Facts) { f.BlockingKnown = false }, gate.ClauseBlocking},
		"approval for an older commit": {func(f *gate.Facts) { f.Approvals[0].Commit = "def5678" }, gate.ClauseApproval},
		"approval by the author": {func(f *gate.Facts) {
			f.Approvals[0].Login = "aarush323"
			f.ApproversForChangedDomains = []string{"aarush323"}
		}, gate.ClauseApproval},
		"approver not for the domain": {func(f *gate.Facts) { f.ApproversForChangedDomains = []string{"ana"} }, gate.ClauseApproval},
		"review not an approval":      {func(f *gate.Facts) { f.Approvals[0].Approved = false }, gate.ClauseApproval},
		"no approval recorded":        {func(f *gate.Facts) { f.Approvals = nil }, gate.ClauseApproval},
		"policy changed, no owner":    {func(f *gate.Facts) { f.PolicyAtHead.Slop.MaxChangedLines = 999 }, gate.ClausePolicy},
		"policy state unknown":        {func(f *gate.Facts) { f.PolicyKnown = false }, gate.ClausePolicy},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := green()
			c.flip(&f)
			d := gate.Evaluate(f)
			require.False(t, d.OK)
			require.Equal(t, []gate.ClauseID{c.want}, unmetIDs(d))
		})
	}
}

func TestAnApprovalCountsOnlyOnTheHeadCommitFromAnotherPersonWhoOwnsTheDomain(t *testing.T) {
	f := green()
	f.Approvals = []gate.Approval{
		{Login: "aarush323", Commit: "abc1234", Approved: true},
		{Login: "ghost", Commit: "abc1234", Approved: true},
		{Login: "rekdfr", Commit: "abc1234", Approved: true},
	}
	require.True(t, gate.Evaluate(f).OK, "one valid approval among invalid ones is enough")
}

func TestTheApprovalReasonNamesTheCommitAndTheLogin(t *testing.T) {
	f := green()
	f.Approvals[0].Login = "rekdfr"
	f.Approvals[0].Commit = "def5678"
	d := gate.Evaluate(f)
	require.Len(t, d.Unmet, 1)
	require.Contains(t, d.Unmet[0].Reason, "approval by rekdfr is for commit def5678 but head is abc1234")
}

func TestASilentPolicyChangeIsFineWhenAPolicyOwnerApproved(t *testing.T) {
	f := green()
	f.PolicyAtHead.Slop.MaxChangedLines = 999
	f.PolicyOwnerApproved = true
	require.True(t, gate.Evaluate(f).OK)
}

func TestEveryFlipFromGreenIsRefused(t *testing.T) {
	flips := []func(*gate.Facts){
		func(f *gate.Facts) { f.TicketLinked = false },
		func(f *gate.Facts) { f.Slop.Pass = false },
		func(f *gate.Facts) { f.Testset.Verified = false },
		func(f *gate.Facts) { f.Testset.ACCovered = false },
		func(f *gate.Facts) { f.Testset.Sensitive = true; f.Testset.Approved = false },
		func(f *gate.Facts) { f.HeadTestTreeHash = "other" },
		func(f *gate.Facts) { f.TestDiffOutsideKey = []string{"x"} },
		func(f *gate.Facts) { f.CI[gate.CheckAcceptance] = "neutral" },
		func(f *gate.Facts) { f.ReviewRequests = []gate.ReviewRequest{{Reviewer: "a"}} },
		func(f *gate.Facts) { f.Blocking = []id.FindingID{"fnd_1"} },
		func(f *gate.Facts) { f.Approvals[0].Commit = "zzz" },
		func(f *gate.Facts) { f.Approvals[0].Login = f.AuthorLogin },
		func(f *gate.Facts) { f.ApproversForChangedDomains = nil },
		func(f *gate.Facts) { f.PolicyAtHead.Version = 2 },
	}
	for i, flip := range flips {
		f := green()
		flip(&f)
		require.False(t, gate.Evaluate(f).OK, "flip %d must refuse", i)
	}
}

func TestBlockingSetHonoursSeverityTierAndStateAndNeverIncludesE4(t *testing.T) {
	minimum := policy.Blocking{MinSeverity: finding.SeverityMedium, MinTier: finding.TierE3}
	cases := map[string]struct {
		in   gate.FindingState
		want bool
	}{
		"E3 at the minimum severity blocks":      {gate.FindingState{ID: "fnd_1", Tier: finding.TierE3, Severity: finding.SeverityMedium, State: finding.StateOpen}, true},
		"E1 above the minimum blocks":            {gate.FindingState{ID: "fnd_2", Tier: finding.TierE1, Severity: finding.SeverityHigh, State: finding.StateUpheld}, true},
		"E3 below the minimum severity does not": {gate.FindingState{ID: "fnd_3", Tier: finding.TierE3, Severity: finding.SeverityLow, State: finding.StateOpen}, false},
		"E4 high severity never blocks":          {gate.FindingState{ID: "fnd_4", Tier: finding.TierE4, Severity: finding.SeverityCritical, State: finding.StateOpen}, false},
		"consensus-blocking E4 never blocks":     {gate.FindingState{ID: "fnd_5", Tier: finding.TierE4, Severity: finding.SeverityCritical, State: finding.StateOpen, Consensus: true}, false},
		"consensus-blocking E2 blocks":           {gate.FindingState{ID: "fnd_6", Tier: finding.TierE2, Severity: finding.SeverityLow, State: finding.StateOpen, Consensus: true}, true},
		"withdrawn finding does not block":       {gate.FindingState{ID: "fnd_7", Tier: finding.TierE1, Severity: finding.SeverityCritical, State: finding.StateWithdrawn}, false},
		"resolved finding does not block":        {gate.FindingState{ID: "fnd_8", Tier: finding.TierE1, Severity: finding.SeverityCritical, State: finding.StateResolved}, false},
		"unverified E4 proposal does not block":  {gate.FindingState{ID: "fnd_9", Tier: finding.TierE4, Severity: finding.SeverityHigh, State: finding.StateUnverified}, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := gate.BlockingSet([]gate.FindingState{c.in}, minimum)
			require.Equal(t, c.want, len(got) == 1, name)
		})
	}
}

func TestBlockingSetReturnsTheIDsInTheOrderGiven(t *testing.T) {
	minimum := policy.Blocking{MinSeverity: finding.SeverityLow, MinTier: finding.TierE3}
	in := []gate.FindingState{
		{ID: "fnd_b", Tier: finding.TierE2, Severity: finding.SeverityHigh, State: finding.StateOpen},
		{ID: "fnd_a", Tier: finding.TierE4, Severity: finding.SeverityHigh, State: finding.StateOpen},
		{ID: "fnd_c", Tier: finding.TierE3, Severity: finding.SeverityLow, State: finding.StateOpen},
	}
	require.Equal(t, []id.FindingID{"fnd_b", "fnd_c"}, gate.BlockingSet(in, minimum))
}

func TestTheCheckAcceptanceNameIsTheSpecName(t *testing.T) {
	require.Equal(t, "heimdall/acceptance", gate.CheckAcceptance)
	require.True(t, strings.HasPrefix(gate.CheckAcceptance, "heimdall/"))
}

// Approval is needed only for a sensitive test set. A non-sensitive set with no approval still holds.
func TestANonSensitiveTestSetNeedsNoApproval(t *testing.T) {
	f := green()
	f.Testset.Sensitive = false
	f.Testset.Approved = false
	require.True(t, gate.Evaluate(f).OK)
}

// A review request holds only while it is neither covered on head nor waived.
func TestACoveredOrWaivedReviewRequestDoesNotBlock(t *testing.T) {
	for _, r := range []gate.ReviewRequest{{Reviewer: "ana", Covered: true}, {Reviewer: "ana", Waived: true}} {
		f := green()
		f.ReviewRequests = []gate.ReviewRequest{r}
		require.True(t, gate.Evaluate(f).OK, "%+v", r)
	}
}
