package gateuse_test

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/gateuse"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// checkJSON is the JSON shape both gateuse and the outbox executor must agree on.
type checkJSON struct {
	Repo       change.Repo         `json:"repo"`
	SHA        id.SHA              `json:"sha"`
	Name       string              `json:"name"`
	Status     ports.CheckStatus   `json:"status"`
	Conclusion change.CIConclusion `json:"conclusion"`
	Summary    string              `json:"summary"`
}

func newEval(t *testing.T, store *fakeStore, out gateuse.Outside) *gateuse.Evaluator {
	t.Helper()
	eval, err := gateuse.New(gateuse.Config{
		Events: store,
		Source: &fakeSource{outside: out},
		Clock:  fixedClock{},
		IDs:    &fakeIDs{},
	})
	require.NoError(t, err)
	return eval
}

func checkPayloadOf(t *testing.T, store *fakeStore) checkJSON {
	t.Helper()
	require.Len(t, store.lastEffects, 1)
	// Every producer must write what the executor's strict decoder accepts.
	_, err := change.DecodeCheckPayload(store.lastEffects[0].Payload)
	require.NoError(t, err, "the gate check effect must be accepted by the shared github_check decoder")
	var c checkJSON
	require.NoError(t, json.Unmarshal(store.lastEffects[0].Payload, &c))
	return c
}

func TestEvaluateAllClausesPass(t *testing.T) {
	res, store := evaluate(t, greenStream(t), greenOutside())

	require.True(t, res.OK)
	require.True(t, res.Appended)
	require.Equal(t, ports.CheckCompleted, res.Status)
	require.Equal(t, int64(11), res.Version)
	require.Equal(t, int64(10), store.lastExpected)
	require.Equal(t, 1, store.appendCalls)
	require.Len(t, store.lastEvents, 1)
	require.Len(t, store.lastEffects, 1)

	ev := producedEvent(t, store, change.EventGateEvaluated)
	p, err := change.Decode(ev)
	require.NoError(t, err)
	ge, ok := p.(change.GateEvaluated)
	require.True(t, ok)
	require.True(t, ge.OK)
	require.Empty(t, ge.Reasons)
	require.Equal(t, head, ge.SHA)
	require.Equal(t, testTree1, ge.TreeHash)
	require.Equal(t, 3, ge.PolicyVersion)

	fx := store.lastEffects[0]
	require.Equal(t, change.EffectGitHubCheck, fx.Kind)
	require.Equal(t, "acme/pay", fx.Target)
	require.Equal(t, change.EffectKey("github_check:PAY-1:11:acme/pay:1111111111111111111111111111111111111111:3"), fx.Key)

	c := checkPayloadOf(t, store)
	require.Equal(t, change.Repo("acme/pay"), c.Repo)
	require.Equal(t, id.SHA("1111111111111111111111111111111111111111"), c.SHA)
	require.Equal(t, "heimdall/gate", c.Name)
	require.Equal(t, ports.CheckCompleted, c.Status)
	require.Equal(t, change.CISuccess, c.Conclusion)
	require.Equal(t, "the merge rule passed", c.Summary)
	require.LessOrEqual(t, len(c.Summary), 2000)
}

// TestEachClauseCanFailInTheUseCase is the flip table: each clause is made to fail from the event stream or the
// outside facts, and the reason names that clause.
func TestEachClauseCanFailInTheUseCase(t *testing.T) {
	cases := map[string]struct {
		build func(*stream) gateuse.Outside
		want  gate.ClauseID
	}{
		"ticket not linked": {
			func(s *stream) gateuse.Outside {
				s.replace(change.EventPROpened, change.PROpened{
					Repo: prRepo, Number: prNumber, HeadSHA: head, Author: author,
				})
				return greenOutside()
			}, gate.ClauseTicketLinked,
		},
		"slop failed": {
			func(s *stream) gateuse.Outside {
				s.replace(change.EventSlopEvaluated, change.SlopEvaluated{
					Pass: false, FailingRules: []string{"size"},
				})
				return greenOutside()
			}, gate.ClauseSlopPassed,
		},
		"tests not verified": {
			func(s *stream) gateuse.Outside { s.drop(change.EventTestsVerified); return greenOutside() },
			gate.ClauseTestset,
		},
		"tests not committed": {
			func(s *stream) gateuse.Outside { s.drop(change.EventTestsCommitted); return greenOutside() },
			gate.ClauseTestset,
		},
		"tests not pinned": {
			func(s *stream) gateuse.Outside { s.drop(change.EventTestsCommitted); return greenOutside() },
			gate.ClauseTestTreePinned,
		},
		"tests changed after pin": {
			func(*stream) gateuse.Outside {
				out := greenOutside()
				out.HeadTestTreeHash = string(testTree2) // the repository at head holds other tests than the pinned ones
				return out
			}, gate.ClauseTestTreePinned,
		},
		"head test tree not read": {
			func(*stream) gateuse.Outside {
				out := greenOutside()
				out.HeadTestTreeHash = ""
				return out
			}, gate.ClauseTestTreePinned,
		},
		"sensitive domain without sign-off": {
			func(s *stream) gateuse.Outside {
				s.replace(change.EventTestsVerified, change.TestsVerified{
					RunIDs: []id.RunID{runID}, SHA: head, TreeHash: testTree1, NeedsSignoff: true,
				})
				return greenOutside()
			}, gate.ClauseTestset,
		},
		"test diff unknown": {
			func(*stream) gateuse.Outside {
				out := greenOutside()
				out.TestDiffKnown = false
				return out
			}, gate.ClauseTestDiffScoped,
		},
		"tests edited outside the key": {
			func(*stream) gateuse.Outside {
				out := greenOutside()
				out.TestDiffOutsideKey = []string{"tests/heimdall/PAY-2/pay_test.go"}
				return out
			}, gate.ClauseTestDiffScoped,
		},
		"acceptance check red": {
			func(s *stream) gateuse.Outside {
				s.replace(change.EventCIReported, change.CIReported{
					Check: gate.CheckAcceptance, SHA: head, Conclusion: change.CIFailure,
				})
				return greenOutside()
			}, gate.ClauseCI,
		},
		"acceptance check missing": {
			func(s *stream) gateuse.Outside { s.drop(change.EventCIReported); return greenOutside() },
			gate.ClauseCI,
		},
		"CI green on an old sha": {
			func(s *stream) gateuse.Outside {
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
			func(*stream) gateuse.Outside {
				out := greenOutside()
				out.CIRequired = []string{"lint"}
				return out
			}, gate.ClauseCI,
		},
		"reviews not known": {
			func(s *stream) gateuse.Outside { s.drop(change.EventReviewersPicked); return greenOutside() },
			gate.ClauseReviews,
		},
		"review not covered": {
			func(s *stream) gateuse.Outside { s.drop(change.EventReviewWaived); return greenOutside() },
			gate.ClauseReviews,
		},
		"blocking finding open": {
			func(s *stream) gateuse.Outside {
				s.add(change.EventFindingFiled, systemActor, change.FindingFiled{
					FindingID: findingID, Ref: ref(1), Tier: finding.TierE2, State: finding.StateOpen, By: "reviewer",
				})
				return greenOutside()
			}, gate.ClauseBlocking,
		},
		"human case pending": {
			func(s *stream) gateuse.Outside {
				s.add(change.EventCaseOpened, systemActor, change.CaseOpened{CaseID: caseID, Finding: ref(1)})
				return greenOutside()
			}, gate.ClauseBlocking,
		},
		"no approval": {
			func(s *stream) gateuse.Outside { s.drop(change.EventApprovalRecorded); return greenOutside() },
			gate.ClauseApproval,
		},
		"approval by the author": {
			func(s *stream) gateuse.Outside {
				s.replace(change.EventApprovalRecorded, change.ApprovalRecorded{Login: author, CommitSHA: head})
				out := greenOutside()
				out.ApproversForChangedDomains = []string{string(author)}
				return out
			}, gate.ClauseApproval,
		},
		"approval on an old sha": {
			func(s *stream) gateuse.Outside {
				s.replace(change.EventApprovalRecorded, change.ApprovalRecorded{Login: approver, CommitSHA: oldHead})
				return greenOutside()
			}, gate.ClauseApproval,
		},
		"approval invalidated by a push": {
			func(s *stream) gateuse.Outside {
				s.add(change.EventPRPushed, githubActor, change.PRPushed{
					Repo: prRepo, Number: prNumber, HeadSHA: nextHead,
				})
				return greenOutside()
			}, gate.ClauseApproval,
		},
		"approver not for the domain": {
			func(*stream) gateuse.Outside {
				out := greenOutside()
				out.ApproversForChangedDomains = nil
				return out
			}, gate.ClauseApproval,
		},
		"policy unknown": {
			func(*stream) gateuse.Outside {
				out := greenOutside()
				out.PolicyKnown = false
				return out
			}, gate.ClausePolicy,
		},
		"policy changed without an owner": {
			func(*stream) gateuse.Outside {
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
			res, _ := evaluate(t, s, out)
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

	res, _ := evaluate(t, s, out)
	require.True(t, res.OK)
}

func TestEvaluateAcceptsATicketLinkedAfterThePROpened(t *testing.T) {
	s := greenStream(t).
		replace(change.EventPROpened, change.PROpened{
			Repo: prRepo, Number: prNumber, HeadSHA: head, Author: author,
		}).
		add(change.EventPRLinked, githubActor, change.PRLinked{
			Repo: prRepo, Number: prNumber, Ticket: changeKey,
		})

	res, _ := evaluate(t, s, greenOutside())
	require.True(t, res.OK)
}

func TestEvaluatePublishesAStatusForEachSituation(t *testing.T) {
	cases := map[string]struct {
		build  func(*stream) gateuse.Outside
		status ports.CheckStatus
		concl  change.CIConclusion
	}{
		"all green": {
			func(*stream) gateuse.Outside { return greenOutside() },
			ports.CheckCompleted, change.CISuccess,
		},
		"tests pending": {
			func(s *stream) gateuse.Outside { s.drop(change.EventTestsVerified); return greenOutside() },
			ports.CheckInProgress, "",
		},
		"tests not committed": {
			func(s *stream) gateuse.Outside { s.drop(change.EventTestsCommitted); return greenOutside() },
			ports.CheckInProgress, "",
		},
		"slop pending": {
			func(s *stream) gateuse.Outside { s.drop(change.EventSlopEvaluated); return greenOutside() },
			ports.CheckInProgress, "",
		},
		"a clause failed": {
			func(s *stream) gateuse.Outside { s.drop(change.EventApprovalRecorded); return greenOutside() },
			ports.CheckCompleted, change.CIFailure,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := greenStream(t)
			res, store := evaluate(t, s, c.build(s))
			require.Equal(t, c.status, res.Status)
			require.Equal(t, c.status, checkPayloadOf(t, store).Status)
			require.Equal(t, c.concl, checkPayloadOf(t, store).Conclusion)
		})
	}
}

func TestEvaluateSuppressesAnIdenticalEvaluation(t *testing.T) {
	store := &fakeStore{events: greenStream(t).events()}
	eval := newEval(t, store, greenOutside())

	first, err := eval.Evaluate(context.Background(), changeKey)
	require.NoError(t, err)
	require.True(t, first.Appended)

	second, err := eval.Evaluate(context.Background(), changeKey)
	require.NoError(t, err)
	require.False(t, second.Appended)
	require.Equal(t, first.OK, second.OK)
	require.Equal(t, first.Reasons, second.Reasons)
	require.Equal(t, int64(11), second.Version)
	require.Equal(t, 1, store.appendCalls)
}

func TestEvaluateAppendsWhenTheReasonsChange(t *testing.T) {
	store := &fakeStore{events: greenStream(t).events()}
	eval := newEval(t, store, greenOutside())
	first, err := eval.Evaluate(context.Background(), changeKey)
	require.NoError(t, err)
	require.True(t, first.Appended)

	s := greenStream(t).add(change.EventFindingFiled, systemActor, change.FindingFiled{
		FindingID: findingID, Ref: ref(1), Tier: finding.TierE2, State: finding.StateOpen, By: "reviewer",
	})
	evs := s.events()
	added := evs[len(evs)-1]
	added.Version = int64(len(store.events) + 1)
	store.events = append(store.events, added)

	second, err := eval.Evaluate(context.Background(), changeKey)
	require.NoError(t, err)
	require.True(t, second.Appended)
	require.False(t, second.OK)
	require.True(t, hasClause(second.Reasons, gate.ClauseBlocking))
	require.Equal(t, 2, store.appendCalls)
}

func TestEvaluateRetriesAVersionConflict(t *testing.T) {
	store := &fakeStore{events: greenStream(t).events(), conflicts: 1}
	eval := newEval(t, store, greenOutside())

	res, err := eval.Evaluate(context.Background(), changeKey)
	require.NoError(t, err)
	require.True(t, res.Appended)
	require.Equal(t, 2, store.appendCalls)
	require.Len(t, store.lastEvents, 1)
}

func TestEvaluateGivesUpAfterFiveRetries(t *testing.T) {
	store := &fakeStore{events: greenStream(t).events(), conflicts: 100}
	eval := newEval(t, store, greenOutside())

	_, err := eval.Evaluate(context.Background(), changeKey)
	require.Error(t, err)
	require.ErrorIs(t, err, ports.ErrVersionConflict)
	require.Equal(t, 6, store.appendCalls)
	require.Empty(t, store.lastEvents)
}

func TestEvaluateReturnsErrNoPullRequest(t *testing.T) {
	s := newStream(t).add(change.EventChangeTriggered, systemActor, change.Triggered{
		Ticket: changeKey, Repo: prRepo, Assignee: "ana", Source: change.TriggerManual,
	})
	store := &fakeStore{events: s.events()}
	eval := newEval(t, store, greenOutside())

	_, err := eval.Evaluate(context.Background(), changeKey)
	require.ErrorIs(t, err, gateuse.ErrNoPullRequest)
	require.Zero(t, store.appendCalls)
}

func TestEvaluateRefusesAClosedPullRequest(t *testing.T) {
	s := greenStream(t).add(change.EventPRClosed, githubActor, change.PRClosed{
		Repo: prRepo, Number: prNumber, HeadSHA: head,
	})
	store := &fakeStore{events: s.events()}
	eval := newEval(t, store, greenOutside())

	_, err := eval.Evaluate(context.Background(), changeKey)
	require.ErrorIs(t, err, gateuse.ErrPullRequestClosed)
	require.Zero(t, store.appendCalls)
}

func TestEvaluateWrapsASourceError(t *testing.T) {
	store := &fakeStore{events: greenStream(t).events()}
	eval, err := gateuse.New(gateuse.Config{
		Events: store,
		Source: &fakeSource{err: errBoom},
		Clock:  fixedClock{},
		IDs:    &fakeIDs{},
	})
	require.NoError(t, err)

	_, err = eval.Evaluate(context.Background(), changeKey)
	require.ErrorIs(t, err, errBoom)
	require.Zero(t, store.appendCalls)
}

func TestNewRefusesAMissingPort(t *testing.T) {
	_, err := gateuse.New(gateuse.Config{})
	require.Error(t, err)
	for _, name := range []string{"Events", "Source", "Clock", "IDs"} {
		require.Contains(t, err.Error(), name)
	}
}

func TestEvaluateBoundsTheEventReasonsAndTheCheckSummary(t *testing.T) {
	out := greenOutside()
	for i := 0; i < 100; i++ {
		out.CIRequired = append(out.CIRequired, "check-"+strconv.Itoa(i)+"-"+strings.Repeat("x", 20))
	}
	res, store := evaluate(t, greenStream(t), out)
	require.False(t, res.OK)
	require.Equal(t, ports.CheckCompleted, res.Status)

	ev := producedEvent(t, store, change.EventGateEvaluated)
	p, err := change.Decode(ev)
	require.NoError(t, err)
	ge := p.(change.GateEvaluated)
	require.NotEmpty(t, ge.Reasons)
	for _, reason := range ge.Reasons {
		require.LessOrEqual(t, len(reason), 256)
	}
	require.LessOrEqual(t, len(checkPayloadOf(t, store).Summary), 2000)
}

func TestEvaluateIsDeterministic(t *testing.T) {
	out := greenOutside()
	first, firstStore := evaluate(t, greenStream(t), out)
	second, secondStore := evaluate(t, greenStream(t), out)
	require.Equal(t, first.OK, second.OK)
	require.Equal(t, first.Reasons, second.Reasons)
	require.Equal(t, first.Status, second.Status)
	require.Equal(t, firstStore.lastEffects[0].Key, secondStore.lastEffects[0].Key)
	require.Equal(t, firstStore.lastEvents[0].Payload, secondStore.lastEvents[0].Payload)
}

func TestEvaluateFindingResolvedPasses(t *testing.T) {
	s := greenStream(t).add(change.EventFindingFiled, systemActor, change.FindingFiled{
		FindingID: findingID, Ref: ref(1), Tier: finding.TierE2, State: finding.StateOpen, By: "reviewer",
	})
	resBlocked, _ := evaluate(t, s, greenOutside())
	require.False(t, resBlocked.OK)
	require.True(t, hasClause(resBlocked.Reasons, gate.ClauseBlocking))

	s.add(change.EventFindingStateChanged, systemActor, change.FindingStateChanged{
		FindingID: findingID, Ref: ref(1), Tier: finding.TierE2, State: finding.StateResolved, By: "author",
	})
	resResolved, _ := evaluate(t, s, greenOutside())
	require.True(t, resResolved.OK)
	require.Empty(t, resResolved.Reasons)
}

func TestEvaluateEffectKeyIsStableAndContainsHeadSHA(t *testing.T) {
	out := greenOutside()
	res, store := evaluate(t, greenStream(t), out)
	require.True(t, res.Appended)
	require.Len(t, store.lastEffects, 1)

	key := string(store.lastEffects[0].Key)
	require.Contains(t, key, string(head))
	require.Contains(t, key, "github_check")
	require.Contains(t, key, "PAY-1")

	_, store2 := evaluate(t, greenStream(t), out)
	require.Equal(t, store.lastEffects[0].Key, store2.lastEffects[0].Key)
}

// Compile-time guard: the store and source are the ports the use case names.
var (
	_ ports.EventStore = (*fakeStore)(nil)
	_ gateuse.Source   = (*fakeSource)(nil)
)
