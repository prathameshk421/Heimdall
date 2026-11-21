package reconcile

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/liveness"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const (
	tkt   = id.ChangeID("KAN-5")
	prc   = id.ChangeID("pr-acme-pay-7")
	head1 = "9f1c2ab"
	head2 = "1a2b3c4"
	base  = "7d3e9a1"
	blobX = "00000000000000000000000000000000000000000000000000000000000000aa"
)

func (r *rig) trigger(t *testing.T) {
	r.add(t, tkt, change.EventChangeTriggered, change.Triggered{Ticket: tkt, Repo: "acme/pay", Assignee: "ann", Source: "jira"})
}

func (r *rig) room(t *testing.T, c id.ChangeID) {
	r.add(t, c, change.EventRoomOpened, change.RoomOpened{RoomID: "switch:room-1"})
}

func (r *rig) branch(t *testing.T) {
	r.add(t, tkt, change.EventBranchEnsured, change.BranchEnsured{Repo: "acme/pay", Branch: "KAN-5-x", BaseSHA: base})
}

func (r *rig) submit(t *testing.T) {
	r.add(t, tkt, change.EventTestsSubmitted, change.TestsSubmitted{ACMap: map[string][]string{"AC1": {"T"}},
		Files: []change.TestFile{{Path: "tests/heimdall/KAN-5/a_test.go", Blob: "blob:sha256:" + blobX}}}, blobX)
}

func (r *rig) pr(t *testing.T) {
	r.add(t, prc, change.EventPROpened, change.PROpened{Repo: "acme/pay", Number: 7, HeadSHA: head1, Author: "ann", BaseSHA: base})
	r.add(t, prc, change.EventSlopEvaluated, change.SlopEvaluated{Pass: true})
}

func TestTheRoomThenTheBranchAreEnsuredOnceEach(t *testing.T) {
	r := newRig(t)
	r.trigger(t)
	r.run(t, tkt)
	require.Equal(t, []string{"KAN-5/room/r0"}, r.store.keys())
	r.run(t, tkt)
	require.Equal(t, []string{"KAN-5/room/r0"}, r.store.keys(), "a second Run decides the same job: nothing new")
	require.Equal(t, 1, r.store.count(tkt, change.EventChangeTriggered))

	r.room(t, tkt)
	r.run(t, tkt)
	require.Equal(t, []string{"KAN-5/room/r0", "KAN-5/branch/r0"}, r.store.keys())
}

func TestAVersionConflictReloadsAndDecidesAgain(t *testing.T) {
	r := newRig(t)
	r.pr(t)
	r.clock.set(t0.Add(time.Hour)) // past the pick wait: the pick row records a fact
	r.store.beforeAppend = func() {
		r.add(t, prc, change.EventCIReported, change.CIReported{Check: "go test", SHA: head1, Conclusion: change.CISuccess})
	}
	r.run(t, prc)
	require.Equal(t, 1, r.store.count(prc, change.EventReviewersPicked), "one result, not two")
	require.Equal(t, 1, r.store.count(prc, change.EventCIReported))
}

func TestARejectedSubmissionGetsANewTestsTaskWithTheNextAttempt(t *testing.T) {
	r := newRig(t)
	r.trigger(t)
	r.room(t, tkt)
	r.branch(t)
	r.submit(t)
	r.add(t, tkt, change.EventTestsRejected, change.TestsRejected{Reasons: []string{"too weak"}, SHA: head1})
	r.run(t, tkt)

	got := r.store.payloads(t, tkt, change.EventTaskAssigned)
	require.Len(t, got, 1)
	a := got[0].(change.TaskAssigned)
	require.Equal(t, "tests", a.Kind)
	require.Equal(t, "attempt-2", a.Input)
	require.Equal(t, id.AgentName("test-author"), a.Agent)
	require.Equal(t, t0.Add(2*time.Hour), a.Deadline)
	require.Contains(t, r.store.keys(), "KAN-5/task/"+string(a.TaskID))
}

func TestANewHeadSupersedesEveryLiveReviewAndAssignsThemAgain(t *testing.T) {
	r := newRig(t)
	r.pr(t)
	r.room(t, prc)
	r.add(t, prc, change.EventReviewersPicked, change.ReviewersPicked{People: []change.ActorID{"rev-1", "rev-2", "rev-3"}, By: "heimdall", Method: change.PickDefault})
	r.run(t, prc)
	require.Equal(t, 3, r.store.count(prc, change.EventTaskAssigned))
	first := r.store.payloads(t, prc, change.EventTaskAssigned)

	r.add(t, prc, change.EventPRPushed, change.PRPushed{Repo: "acme/pay", Number: 7, HeadSHA: head2})
	r.add(t, prc, change.EventSlopEvaluated, change.SlopEvaluated{Pass: true})
	r.run(t, prc)
	require.Equal(t, 3, r.store.count(prc, change.EventTaskSuperseded))
	require.Equal(t, 6, r.store.count(prc, change.EventTaskAssigned))
	for _, p := range r.store.payloads(t, prc, change.EventTaskAssigned)[3:] {
		require.Equal(t, "head-"+head2, p.(change.TaskAssigned).Input)
	}
	require.Len(t, first, 3)
}

// heldTask sets up a change with one live review task assigned at t0 and returns its id.
func (r *rig) heldTask(t *testing.T) id.TaskID {
	r.pr(t)
	r.room(t, prc)
	r.add(t, prc, change.EventReviewersPicked, change.ReviewersPicked{People: []change.ActorID{"rev-1"}, By: "heimdall", Method: change.PickDefault})
	r.run(t, prc)
	return r.store.payloads(t, prc, change.EventTaskAssigned)[0].(change.TaskAssigned).TaskID
}

func TestAnUnansweredWakeIsRepeatedOnce(t *testing.T) {
	r := newRig(t)
	task := r.heldTask(t)
	r.clock.set(t0.Add(2 * time.Minute))
	r.run(t, prc)
	require.Zero(t, r.store.count(prc, change.EventTaskRewoken), "inside the ack window")

	r.clock.set(t0.Add(4 * time.Minute))
	r.run(t, prc)
	require.Equal(t, 1, r.store.count(prc, change.EventTaskRewoken))
	require.Contains(t, r.store.keys(), "pr-acme-pay-7/"+string(task)+"/wake/1")
	r.run(t, prc)
	require.Equal(t, 1, r.store.count(prc, change.EventTaskRewoken), "the same run twice changes nothing")
}

func TestAQuietAgentIsProbedWokenAgainFailedAndReassigned(t *testing.T) {
	r := newRig(t)
	task := r.heldTask(t)
	r.add(t, prc, change.EventTaskAcked, change.TaskAcked{TaskID: task, Agent: "rev-1", Role: "reviewer"})

	r.clock.set(t0.Add(11 * time.Minute)) // stalled: the provider is asked first
	r.run(t, prc)
	require.Equal(t, 1, r.health.probes)
	require.Equal(t, 1, r.store.count(prc, change.EventProviderProbed))
	require.Equal(t, "ok", r.store.payloads(t, prc, change.EventProviderProbed)[0].(change.ProviderProbed).Health)

	r.clock.set(t0.Add(12 * time.Minute)) // healthy: wake again. Switch offers an agent no session reset (D172)
	r.run(t, prc)
	require.Contains(t, r.store.keys(), "pr-acme-pay-7/"+string(task)+"/wake/1")
	for _, k := range r.store.keys() {
		require.NotContains(t, k, "/reset/", "no session reset job: Heimdall cannot reset a Switch session")
	}
	require.Equal(t, 1, r.store.count(prc, change.EventTaskRewoken))
	require.True(t, r.store.payloads(t, prc, change.EventTaskRewoken)[0].(change.TaskRewoken).Stall)
	require.Equal(t, 1, r.health.probes, "a healthy provider is not asked again")

	r.clock.set(t0.Add(23 * time.Minute)) // still silent ten minutes after the stall wake
	r.run(t, prc)
	require.Equal(t, 1, r.store.count(prc, change.EventTaskFailed))

	r.run(t, prc) // the row is open again: a fresh task
	require.Equal(t, 2, r.store.count(prc, change.EventTaskAssigned))
}

func TestARefusingProviderBlocksTheWakeAndIsProbedAgain(t *testing.T) {
	r := newRig(t)
	task := r.heldTask(t)
	r.add(t, prc, change.EventTaskAcked, change.TaskAcked{TaskID: task, Agent: "rev-1", Role: "reviewer"})
	r.health.health = liveness.Refusing
	r.clock.set(t0.Add(11 * time.Minute))
	r.run(t, prc)
	r.run(t, prc)
	require.Equal(t, 2, r.health.probes, "a refusing provider is asked on every Run")
	require.Equal(t, 1, r.store.count(prc, change.EventProviderProbed), "only a change of health is recorded")
	require.Zero(t, r.store.count(prc, change.EventTaskRewoken))

	evs, err := r.store.Load(context.Background(), prc)
	require.NoError(t, err)
	v, err := change.Fold(evs)
	require.NoError(t, err)
	needs := change.Missing(change.In{Change: prc, View: v, Settings: r.rec.cfg.Settings, Now: r.clock.Now()})
	require.Contains(t, needs[len(needs)-1].Text, "model provider refuses")

	r.health.health = liveness.Healthy
	r.run(t, prc)
	require.Equal(t, 2, r.store.count(prc, change.EventProviderProbed), "the block lifts when the provider answers")
}

func TestATaskPastItsDeadlineTimesOut(t *testing.T) {
	r := newRig(t)
	r.heldTask(t)
	r.clock.set(t0.Add(31 * time.Minute))
	r.run(t, prc)
	require.Equal(t, 1, r.store.count(prc, change.EventTaskTimedOut))
}

func TestADeadJobIsNotInsertedAgainAndResumeRearmsItWithANewKey(t *testing.T) {
	r := newRig(t)
	r.trigger(t)
	r.run(t, tkt)
	r.store.dead[tkt] = []change.DeadJob{{Key: "KAN-5/room/r0", Kind: change.EffectRoomOpen, Attempts: 8, LastError: "down"}}
	r.run(t, tkt)
	require.Equal(t, []string{"KAN-5/room/r0"}, r.store.keys(), "the same key is a no-op; nothing loops on a dead job")

	rec := Recorder{Store: r.store}
	require.NoError(t, rec.Record(context.Background(), tkt, func(change.View) ([]change.Event, error) {
		return []change.Event{{ID: "3f1c9a52-8d2b-4e7a-9c11-0000000000aa", Type: change.EventStepResumed, Actor: change.Actor{Kind: change.ActorHuman, ID: "ann"},
			OccurredAt: t0, Payload: []byte(`{"step":"open_room","by":"ann"}`)}}, nil
	}))
	r.run(t, tkt)
	require.Equal(t, []string{"KAN-5/room/r0", "KAN-5/room/r1"}, r.store.keys())
}

func TestSweepRunsEveryOpenChangeOnce(t *testing.T) {
	r := newRig(t)
	r.trigger(t)
	r.pr(t)
	require.NoError(t, r.rec.Sweep(context.Background()))
	require.Equal(t, 1, r.store.loads[tkt])
	require.Equal(t, 1, r.store.loads[prc])
}

func TestKickingKicksAfterASuccessfulAppendOnly(t *testing.T) {
	r := newRig(t)
	k := Kicking{EventStore: r.store, Kicker: r.rec}
	ev := func(n int64) change.Event {
		return change.Event{ID: "3f1c9a52-8d2b-4e7a-9c11-0000000000bb", ChangeID: tkt, Version: n, Type: change.EventRoomOpened,
			Actor: change.Actor{Kind: change.ActorSystem, ID: "x"}, OccurredAt: t0, CorrelationID: "c", Payload: []byte(`{"room_id":"switch:r"}`)}
	}
	_, err := k.Append(context.Background(), tkt, 5, []change.Event{ev(6)}, nil)
	require.ErrorIs(t, err, ports.ErrVersionConflict)
	require.Empty(t, r.rec.kicks, "a failed append kicks nothing")
	_, err = k.Append(context.Background(), tkt, 0, []change.Event{ev(1)}, nil)
	require.NoError(t, err)
	require.Equal(t, tkt, <-r.rec.kicks)
}

func TestTwoRunsOfOneChangeSerialize(t *testing.T) {
	r := newRig(t)
	r.pr(t)
	r.clock.set(t0.Add(time.Hour))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			assert := func(err error) {
				if err != nil {
					t.Error(err)
				}
			}
			assert(r.rec.Run(context.Background(), prc))
		}()
	}
	wg.Wait()
	require.Equal(t, 1, r.store.count(prc, change.EventReviewersPicked))
}

func TestAnUnreadableInputStopsTheRunWithAnError(t *testing.T) {
	r := newRig(t)
	r.trigger(t)
	r.rec.cfg.Inputs = brokenInputs{}
	err := r.rec.Run(context.Background(), tkt)
	require.Error(t, err)
	require.Contains(t, err.Error(), "policy")
	require.Empty(t, r.store.keys(), "no row ran on an empty default")
}

type brokenInputs struct{ fakeInputs }

func (brokenInputs) Policy(context.Context, change.Repo, id.SHA) (policy.Policy, error) {
	return policy.Policy{}, context.DeadlineExceeded
}

// A policy that does not decode is a fact about the repository, not a failed read: retrying cannot fix it. The Run
// starts nothing and the change's needs say why, on every change whose base has that policy (B10).
func TestAPolicyThatCannotBeUsedIsANeedAndStartsNothing(t *testing.T) {
	r := newRig(t)
	r.trigger(t)
	r.rec.cfg.Inputs = invalidPolicyInputs{}
	require.NoError(t, r.rec.Run(context.Background(), tkt), "a bad policy file is not an error to retry")
	require.Empty(t, r.store.keys(), "no row ran on an empty default")
	needs, err := r.rec.Needs(context.Background(), tkt)
	require.NoError(t, err)
	require.Len(t, needs, 1)
	require.Contains(t, needs[0], "heimdall.yaml")
	require.Contains(t, needs[0], "version 3 is not known")
	require.Contains(t, needs[0], "nothing runs on this change until the policy is fixed")
}

type invalidPolicyInputs struct{ fakeInputs }

func (invalidPolicyInputs) Policy(context.Context, change.Repo, id.SHA) (policy.Policy, error) {
	return policy.Policy{}, errors.Join(policy.ErrInvalid, errors.New("heimdall.yaml: version 3 is not known"))
}
