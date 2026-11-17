package change_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

func TestEvaluateGateAllClausesPass(t *testing.T) {
	res := evaluate(t, greenStream(t), greenOutside())

	require.True(t, res.OK)
	require.Empty(t, res.Reasons)
	require.Equal(t, change.CheckStatusCompleted, res.Status)
	require.Equal(t, change.CISuccess, res.Conclusion)
	require.Equal(t, testTree1, res.TreeHash)
	require.Equal(t, 3, res.PolicyVersion)
	require.Contains(t, res.Summary, "### ✅ Ready to merge")
	require.Contains(t, res.Summary, "| Approval from an approver | ✅ |")
	require.LessOrEqual(t, len(res.Summary), 2000)
}

// TestEachClauseCanFailInTheUseCase is the flip table: each clause is made to fail from the event stream or the
// outside facts, and the reason names that clause.
func TestEachClauseCanFailInTheUseCase(t *testing.T) {
	cases := map[string]struct {
		build func(*stream) change.Outside
		want  gate.ClauseID
	}{
		"ticket not linked": {
			func(s *stream) change.Outside {
				s.replace(change.EventPROpened, change.PROpened{
					Repo: prRepo, Number: prNumber, HeadSHA: head, Author: author,
				})
				return greenOutside()
			}, gate.ClauseTicketLinked,
		},
		"slop failed": {
			func(s *stream) change.Outside {
				s.replace(change.EventSlopEvaluated, change.SlopEvaluated{
					Pass: false, FailingRules: []string{"size"},
				})
				return greenOutside()
			}, gate.ClauseSlopPassed,
		},
		"tests not verified": {
			func(s *stream) change.Outside { s.drop(change.EventTestsVerified); return greenOutside() },
			gate.ClauseTestset,
		},
		"tests not committed": {
			func(s *stream) change.Outside { s.drop(change.EventTestsCommitted); return greenOutside() },
			gate.ClauseTestset,
		},
		"tests not pinned": {
			func(s *stream) change.Outside { s.drop(change.EventTestsCommitted); return greenOutside() },
			gate.ClauseTestTreePinned,
		},
		"tests changed after pin": {
			func(*stream) change.Outside {
				out := greenOutside()
				out.HeadTestTreeHash = string(testTree2) // the repository at head holds other tests than the pinned ones
				return out
			}, gate.ClauseTestTreePinned,
		},
		"head test tree not read": {
			func(*stream) change.Outside {
				out := greenOutside()
				out.HeadTestTreeHash = ""
				return out
			}, gate.ClauseTestTreePinned,
		},
		"sensitive domain without sign-off": {
			func(s *stream) change.Outside {
				s.replace(change.EventTestsVerified, change.TestsVerified{
					RunIDs: []id.RunID{runID}, SHA: head, TreeHash: testTree1, NeedsSignoff: true,
				})
				return greenOutside()
			}, gate.ClauseTestset,
		},
		"test diff unknown": {
			func(*stream) change.Outside {
				out := greenOutside()
				out.TestDiffKnown = false
				return out
			}, gate.ClauseTestDiffScoped,
		},
		"tests edited outside the key": {
			func(*stream) change.Outside {
				out := greenOutside()
				out.TestDiffOutsideKey = []string{"tests/heimdall/PAY-2/pay_test.go"}
				return out
			}, gate.ClauseTestDiffScoped,
		},
		"acceptance check red": {
			func(s *stream) change.Outside {
				s.replace(change.EventCIReported, change.CIReported{
					Check: gate.CheckAcceptance, SHA: head, Conclusion: change.CIFailure,
				})
				return greenOutside()
			}, gate.ClauseCI,
		},
		"acceptance check missing": {
			func(s *stream) change.Outside { s.drop(change.EventCIReported); return greenOutside() },
			gate.ClauseCI,
		},
		"CI green on an old sha": {
			func(s *stream) change.Outside {
				s.drop(change.EventCIReported).
					add(change.EventCIReported, githubActor, change.CIReported{
						Check: gate.CheckAcceptance, SHA: oldHead, Conclusion: change.CISuccess,
					}).
					add(change.EventCIReported, githubActor, change.CIReported{
						Check: "build", SHA: oldHead, Conclusion: change.CISuccess,
					})
				return greenOutside()
			}, gate.ClauseCI,
		},
		"required check missing": {
			func(*stream) change.Outside {
				out := greenOutside()
				out.CIRequired = []string{"lint"}
				return out
			}, gate.ClauseCI,
		},
		"reviews not known": {
			func(s *stream) change.Outside { s.drop(change.EventReviewersPicked); return greenOutside() },
			gate.ClauseReviews,
		},
		"review not covered": {
			func(s *stream) change.Outside { s.drop(change.EventReviewWaived); return greenOutside() },
			gate.ClauseReviews,
		},
		"blocking finding open": {
			func(s *stream) change.Outside {
				s.add(change.EventFindingFiled, systemActor, change.FindingFiled{
					FindingID: findingID, Ref: ref(1), Tier: finding.TierE2, Severity: "high", State: finding.StateOpen, By: "reviewer",
				})
				return greenOutside()
			}, gate.ClauseBlocking,
		},
		"human case pending": {
			func(s *stream) change.Outside {
				s.add(change.EventCaseOpened, systemActor, change.CaseOpened{CaseID: caseID, Finding: ref(1)})
				return greenOutside()
			}, gate.ClauseBlocking,
		},
		"no approval": {
			func(s *stream) change.Outside { s.drop(change.EventApprovalRecorded); return greenOutside() },
			gate.ClauseApproval,
		},
		"approval by the author": {
			func(s *stream) change.Outside {
				s.replace(change.EventApprovalRecorded, change.ApprovalRecorded{Login: author, CommitSHA: head})
				out := greenOutside()
				out.ApproversForChangedDomains = []string{string(author)}
				return out
			}, gate.ClauseApproval,
		},
		"approval on an old sha": {
			func(s *stream) change.Outside {
				s.replace(change.EventApprovalRecorded, change.ApprovalRecorded{Login: approver, CommitSHA: oldHead})
				return greenOutside()
			}, gate.ClauseApproval,
		},
		"approval invalidated by a push": {
			func(s *stream) change.Outside {
				s.add(change.EventPRPushed, githubActor, change.PRPushed{
					Repo: prRepo, Number: prNumber, HeadSHA: nextHead,
				})
				return greenOutside()
			}, gate.ClauseApproval,
		},
		"approver not for the domain": {
			func(*stream) change.Outside {
				out := greenOutside()
				out.ApproversForChangedDomains = nil
				return out
			}, gate.ClauseApproval,
		},
		"policy unknown": {
			func(*stream) change.Outside {
				out := greenOutside()
				out.PolicyKnown = false
				return out
			}, gate.ClausePolicy,
		},
		"policy changed without an owner": {
			func(*stream) change.Outside {
				out := greenOutside()
				out.PolicyAtHead = policy.Policy{Version: 1, Workflow: "other"}
				return out
			}, gate.ClausePolicy,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := greenStream(t)
			out := c.build(s)
			res := evaluate(t, s, out)
			require.False(t, res.OK)
			require.True(t, hasClause(res.Reasons, c.want), "reasons: %v", res.Reasons)
		})
	}
}

func TestEvaluateLetsAPolicyOwnerApproveThePolicyChange(t *testing.T) {
	s := greenStream(t)
	out := greenOutside()
	out.PolicyAtHead = policy.Policy{Version: 1, Workflow: "other"}
	out.PolicyOwnerApproved = true

	require.True(t, evaluate(t, s, out).OK)
}

func TestEvaluateAcceptsATicketLinkedAfterThePROpened(t *testing.T) {
	s := greenStream(t).
		replace(change.EventPROpened, change.PROpened{
			Repo: prRepo, Number: prNumber, HeadSHA: head, Author: author,
		}).
		add(change.EventPRLinked, githubActor, change.PRLinked{
			Repo: prRepo, Number: prNumber, Ticket: changeKey,
		})

	require.True(t, evaluate(t, s, greenOutside()).OK)
}

func TestEvaluateGivesAStatusForEachSituation(t *testing.T) {
	cases := map[string]struct {
		build  func(*stream) change.Outside
		status change.CheckStatus
		concl  change.CIConclusion
	}{
		"all green": {
			func(*stream) change.Outside { return greenOutside() },
			change.CheckStatusCompleted, change.CISuccess,
		},
		"tests pending": {
			func(s *stream) change.Outside { s.drop(change.EventTestsVerified); return greenOutside() },
			change.CheckStatusInProgress, "",
		},
		"tests not committed": {
			func(s *stream) change.Outside { s.drop(change.EventTestsCommitted); return greenOutside() },
			change.CheckStatusInProgress, "",
		},
		"slop pending": {
			func(s *stream) change.Outside { s.drop(change.EventSlopEvaluated); return greenOutside() },
			change.CheckStatusInProgress, "",
		},
		"a clause failed": {
			func(s *stream) change.Outside { s.drop(change.EventApprovalRecorded); return greenOutside() },
			change.CheckStatusCompleted, change.CIFailure,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := greenStream(t)
			res := evaluate(t, s, c.build(s))
			require.Equal(t, c.status, res.Status)
			require.Equal(t, c.concl, res.Conclusion)
		})
	}
}

func TestEvaluateBoundsTheEventReasonsAndTheCheckSummary(t *testing.T) {
	out := greenOutside()
	for i := 0; i < 100; i++ {
		out.CIRequired = append(out.CIRequired, "check-"+strconv.Itoa(i)+"-"+strings.Repeat("x", 20))
	}
	res := evaluate(t, greenStream(t), out)
	require.False(t, res.OK)
	require.Equal(t, change.CheckStatusCompleted, res.Status)
	require.NotEmpty(t, res.Reasons)
	for _, reason := range res.Reasons {
		require.LessOrEqual(t, len(reason), 256)
	}
	require.LessOrEqual(t, len(res.Summary), change.MaxCheckSummary)
}

func TestEvaluateIsDeterministic(t *testing.T) {
	first := evaluate(t, greenStream(t), greenOutside())
	second := evaluate(t, greenStream(t), greenOutside())
	require.Equal(t, first, second)
}

func TestEvaluateFindingResolvedPasses(t *testing.T) {
	s := greenStream(t).add(change.EventFindingFiled, systemActor, change.FindingFiled{
		FindingID: findingID, Ref: ref(1), Tier: finding.TierE2, Severity: "high", State: finding.StateOpen, By: "reviewer",
	})
	resBlocked := evaluate(t, s, greenOutside())
	require.False(t, resBlocked.OK)
	require.True(t, hasClause(resBlocked.Reasons, gate.ClauseBlocking))

	s.add(change.EventFindingStateChanged, systemActor, change.FindingStateChanged{
		FindingID: findingID, Ref: ref(1), Tier: finding.TierE2, Severity: "high", State: finding.StateResolved, By: "author",
	})
	resResolved := evaluate(t, s, greenOutside())
	require.True(t, resResolved.OK)
	require.Empty(t, resResolved.Reasons)
}

// The gate row publishes the decision once per result: a fact and the heimdall/gate check keyed by what they say; the
// same result on the same head publishes nothing; a different result publishes under a different key.
func TestTheGateRowPublishesTheDecisionOncePerResult(t *testing.T) {
	s := greenStream(t)
	view := func() change.View {
		v, err := change.Fold(s.events())
		require.NoError(t, err)
		return v
	}
	rowGate := func(v change.View) []change.Action {
		var out []change.Action
		for _, ra := range change.Next(change.In{Change: changeKey, View: v, Outside: greenOutside()}) {
			if ra.Row == "gate" {
				out = append(out, ra.Action)
			}
		}
		return out
	}
	first := rowGate(view())
	require.Len(t, first, 2)
	event, ok := first[0].Fact.(change.GateEvaluated)
	require.True(t, ok)
	require.True(t, event.OK)
	require.Equal(t, head, event.SHA)
	require.Equal(t, testTree1, event.TreeHash)
	job := first[1].Job
	require.Equal(t, change.EffectGitHubCheck, job.Kind)
	require.Equal(t, "acme/pay", job.Target)
	check := job.Payload.(change.CheckPayload)
	require.Equal(t, "heimdall/gate", check.Name)
	require.Equal(t, change.CISuccess, check.Conclusion)
	require.NoError(t, check.Validate())
	require.Contains(t, string(job.Key), string(head))

	// The fact is recorded: the same result is not published again.
	s.add(change.EventGateEvaluated, systemActor, event)
	require.Empty(t, rowGate(view()))

	// A different result is a new fact under a new key.
	s.drop(change.EventApprovalRecorded)
	second := rowGate(view())
	require.Len(t, second, 2)
	require.False(t, second[0].Fact.(change.GateEvaluated).OK)
	require.NotEqual(t, job.Key, second[1].Job.Key)
}
