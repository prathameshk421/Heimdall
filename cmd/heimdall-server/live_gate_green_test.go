//go:build live

package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/github"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/policyyaml"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/postgres"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/gateuse"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/outbox"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/ids"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// TestLiveGateGreenPath tests the success path of the merge gate against the real GitHub API
// on fixture pull request #2 ("PAY-200: refund limit test") in repository aarush323/heimdall-fixture-pay.
// It is not part of make fast.
//
// Credentials:
// The test reads credentials from the repository's .env file at runtime:
//   - GITHUB_APP_ID: integer GitHub App ID
//   - GITHUB_APP_INSTALLATION_ID: integer installation ID
//   - GITHUB_APP_PRIVATE_KEY_PATH: path to the App's private key PEM file
// If any of these values are missing or the key file cannot be read, the test skips cleanly.
// Nothing from the credential file or environment is printed or logged.
//
// What this test writes to GitHub:
// Exactly one check run named "heimdall/gate" on the head commit of PR #4 in aarush323/heimdall-fixture-pay
// (via GitHub Checks API). No other files, comments, or branches are modified.
//
// How to run:
//   CGO_ENABLED=0 go test -tags live -count=1 -run TestLiveGateGreenPath -v ./cmd/heimdall-server/

func liveGreenFindPR(t *testing.T, ghAdapter *github.Adapter, number int) ports.PR {
	t.Helper()
	ctx := context.Background()
	openPRs, err := ghAdapter.OpenPRs(ctx, liveGateFixtureRepo)
	if err != nil {
		t.Skipf("cannot list open pull requests on %s: %v", liveGateFixtureRepo, err)
	}
	for _, p := range openPRs {
		if p.Number == number {
			return p
		}
	}
	t.Skipf("PR #%d is missing from open pull requests on %s: live test skipped", number, liveGateFixtureRepo)
	return ports.PR{}
}

func liveGreenCheckApproval(t *testing.T, ghAdapter *github.Adapter, pr ports.PR) {
	t.Helper()
	ctx := context.Background()
	reviews, err := ghAdapter.PRReviews(ctx, liveGateFixtureRepo, pr.Number)
	if err != nil {
		t.Skipf("cannot read reviews on %s#%d: %v", liveGateFixtureRepo, pr.Number, err)
	}
	for _, r := range reviews {
		if r.CommitSHA == pr.HeadSHA && r.State == ports.ReviewStateApproved && r.Login != pr.Author {
			return
		}
	}
	t.Skipf("no approval by a non-author exists on head %s of PR #%d: live test skipped", pr.HeadSHA, pr.Number)
}

func liveGreenMakeEvent(t *testing.T, gen ports.IDs, changeID id.ChangeID, v int64, et change.EventType, actor change.Actor, p change.Payload, now time.Time) change.Event {
	t.Helper()
	require.NoError(t, p.Validate(), "payload validation for %s", et)
	raw, err := json.Marshal(p)
	require.NoError(t, err, "marshal payload for %s", et)
	eid := gen.NewEvent()
	ev := change.Event{
		ID:            eid,
		ChangeID:      changeID,
		Version:       v,
		Type:          et,
		Actor:         actor,
		OccurredAt:    now,
		CorrelationID: change.CorrelationID(eid.String()),
		Payload:       raw,
	}
	require.NoError(t, ev.Validate(), "envelope validation for %s", et)
	_, err = change.Decode(ev)
	require.NoError(t, err, "decode validation for %s", et)
	return ev
}

func liveGreenSeedStream(t *testing.T, pool *pgxpool.Pool, gen ports.IDs, pr ports.PR, treeHash id.SHA) id.ChangeID {
	t.Helper()
	ticket := id.ChangeID(strings.TrimSpace(strings.SplitN(pr.Title, ":", 2)[0]))
	require.NotEmpty(t, ticket, "ticket must not be empty from PR title %q", pr.Title)

	now := time.Now().UTC()
	taskID := gen.NewTask()
	bundleRef := workflow.BlobRef("blob:sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")

	evs := []change.Event{
		liveGreenMakeEvent(t, gen, ticket, 1, change.EventChangeTriggered,
			change.Actor{Kind: change.ActorSystem, ID: "engine"},
			change.Triggered{Ticket: ticket, Repo: liveGateFixtureRepo, Assignee: "prathameshk421", Source: change.TriggerManual},
			now,
		),
		liveGreenMakeEvent(t, gen, ticket, 2, change.EventRoomOpened,
			change.Actor{Kind: change.ActorSystem, ID: "switch"},
			change.RoomOpened{RoomID: "room-200"},
			now,
		),
		liveGreenMakeEvent(t, gen, ticket, 3, change.EventTaskAssigned,
			change.Actor{Kind: change.ActorSystem, ID: "engine"},
			change.TaskAssigned{
				TaskID:    taskID,
				Agent:     "test-author",
				Role:      workflow.RoleTestAuthor,
				Deadline:  now.Add(time.Hour),
				BundleRef: bundleRef,
			},
			now,
		),
		liveGreenMakeEvent(t, gen, ticket, 4, change.EventTestsSubmitted,
			change.Actor{Kind: change.ActorAgent, ID: "test-author"},
			change.TestsSubmitted{
				Refs:     []string{"tests/heimdall/PAY-200/refund_limit_test.go"},
				ACMap:    map[string][]string{"AC1": {"tests/heimdall/PAY-200/refund_limit_test.go"}},
				SHA:      pr.HeadSHA,
				TreeHash: treeHash,
			},
			now,
		),
		liveGreenMakeEvent(t, gen, ticket, 5, change.EventTestsVerified,
			change.Actor{Kind: change.ActorSystem, ID: "sandbox"},
			change.TestsVerified{
				RunIDs:       []id.RunID{gen.NewRun()},
				SHA:          pr.HeadSHA,
				TreeHash:     treeHash,
				NeedsSignoff: false,
			},
			now,
		),
		liveGreenMakeEvent(t, gen, ticket, 6, change.EventTestsCommitted,
			change.Actor{Kind: change.ActorSystem, ID: "engine"},
			change.TestsCommitted{
				SHA:      pr.HeadSHA,
				TreeHash: treeHash,
			},
			now,
		),
	}

	state, err := change.Replay(evs)
	require.NoError(t, err, "replay seeded events")
	require.Equal(t, change.StateBuilding, state, "seeded events must transition stream to BUILDING")

	store := postgres.NewStore(pool)
	_, err = store.Append(context.Background(), ticket, 0, evs, nil)
	require.NoError(t, err, "seed initial events for %s", ticket)

	return ticket
}

func liveGreenAppendReview(t *testing.T, pool *pgxpool.Pool, gen ports.IDs, changeID id.ChangeID, pr ports.PR) {
	t.Helper()
	events, _ := liveGateLoadStream(t, pool, pr.Number)
	require.NotEmpty(t, events, "stream must not be empty before appending review")
	lastVersion := events[len(events)-1].Version

	reviewerActor := change.ActorID("reviewer-1")
	reviewerAgent := id.AgentName("reviewer-1")
	now := time.Now().UTC()

	evPicked := liveGreenMakeEvent(t, gen, changeID, lastVersion+1, change.EventReviewersPicked,
		change.Actor{Kind: change.ActorSystem, ID: "engine"},
		change.ReviewersPicked{
			People: []change.ActorID{reviewerActor},
			By:     "engine",
			Method: change.PickDefault,
		},
		now,
	)

	evSubmitted := liveGreenMakeEvent(t, gen, changeID, lastVersion+2, change.EventReviewSubmitted,
		change.Actor{Kind: change.ActorAgent, ID: reviewerActor},
		change.ReviewSubmitted{
			Reviewer:    reviewerAgent,
			FindingRefs: []id.FindingRef{},
			PackDate:    now.Format("2006-01-02"),
		},
		now,
	)

	// The product's own acceptance run (the sandbox runs the pinned tests on the head) reports this check. The sandbox
	// path is not wired yet, so the test seeds its result, as it seeds the review.
	evAcceptance := liveGreenMakeEvent(t, gen, changeID, lastVersion+3, change.EventCIReported,
		change.Actor{Kind: change.ActorGitHub, ID: "github"},
		change.CIReported{Check: "heimdall/acceptance", SHA: pr.HeadSHA, Conclusion: change.CISuccess},
		now,
	)

	store := postgres.NewStore(pool)
	_, err := store.Append(context.Background(), changeID, lastVersion, []change.Event{evPicked, evSubmitted, evAcceptance}, nil)
	require.NoError(t, err, "append reviewers.picked and review.submitted for %s", changeID)
}

func TestLiveGateGreenPath(t *testing.T) {
	ghAdapter := liveGateAdapter(t)
	pr := liveGreenFindPR(t, ghAdapter, 4)
	liveGreenCheckApproval(t, ghAdapter, pr)

	appInstance, pool := liveGateBuildApp(t, ghAdapter)
	ctx := context.Background()

	treePath := "tests/heimdall/PAY-200"
	treeHash, err := ghAdapter.TreeHash(ctx, liveGateFixtureRepo, pr.HeadSHA, treePath)
	require.NoError(t, err, "read git tree hash of %s at head %s", treePath, pr.HeadSHA)
	require.NotEmpty(t, treeHash, "tree hash of %s must not be empty", treePath)
	t.Logf("Read tree hash for %s at %s: %s", treePath, pr.HeadSHA, treeHash)

	gen := ids.New()
	changeKey := liveGreenSeedStream(t, pool, gen, pr, treeHash)

	syncRes, err := appInstance.syncer.SyncOnce(ctx)
	require.NoError(t, err, "syncer.SyncOnce failed")
	// The repository also holds PR #1 (ticket PAY-142), which has no change in this fresh database: intake refuses
	// it, as it should. Only a failure of the PR under test is an error here.
	for _, f := range syncRes.Failed {
		require.NotEqual(t, pr.Number, f.Number, "the sync of PR #%d under test failed: %v", pr.Number, f)
	}
	require.Positive(t, syncRes.Synced, "expected at least one PR to be synced")

	eventsAfterSync, streamID := liveGateLoadStream(t, pool, pr.Number)
	require.Equal(t, changeKey, streamID, "stream ID must match change key")

	var slopEval *change.SlopEvaluated
	for _, e := range eventsAfterSync {
		if e.Type == change.EventSlopEvaluated {
			var se change.SlopEvaluated
			if err := json.Unmarshal(e.Payload, &se); err == nil {
				slopEval = &se
			}
		}
	}
	require.NotNil(t, slopEval, "slop.evaluated must be present in stream")
	t.Logf("slop pass=%t failing rules=%v", slopEval.Pass, slopEval.FailingRules)
	require.True(t, slopEval.Pass, "slop rule must pass")
	require.Empty(t, slopEval.FailingRules, "slop failing rules must be empty on pass")

	syncAnalysis := liveGateAnalyzeEvents(eventsAfterSync, pr)
	require.True(t, syncAnalysis.approvedByNonAuthor, "one non-author approval on head must be recorded")
	t.Logf("Recorded approval by non-author login: %s", syncAnalysis.approverLogin)
	if syncAnalysis.lastGateEval != nil {
		t.Logf("Sync pass gate evaluated OK: %t", syncAnalysis.lastGateEval.OK)
		t.Logf("Sync pass gate reasons count: %d", len(syncAnalysis.lastGateEval.Reasons))
		for i, r := range syncAnalysis.lastGateEval.Reasons {
			t.Logf("Sync pass gate reason [%d]: %s", i+1, r)
		}
	}

	liveGreenAppendReview(t, pool, gen, changeKey, pr)

	gateSrc, err := gateuse.NewSource(gateuse.SourceConfig{
		PRs:    ghAdapter,
		Code:   ghAdapter,
		Decode: policyyaml.Decode,
	})
	require.NoError(t, err, "build gate source")

	gateEval, err := gateuse.New(gateuse.Config{
		Events: postgres.NewStore(pool),
		Source: gateSrc,
		Clock:  clock.System{},
		IDs:    gen,
	})
	require.NoError(t, err, "build gate evaluator")

	evalRes, err := gateEval.Evaluate(ctx, changeKey)
	require.NoError(t, err, "direct gate evaluator Evaluate for %s", changeKey)
	t.Logf("Direct gate evaluation returned OK: %t, Appended: %t, Status: %s, Reasons count: %d",
		evalRes.OK, evalRes.Appended, evalRes.Status, len(evalRes.Reasons))
	for i, r := range evalRes.Reasons {
		t.Logf("Direct gate reason [%d]: %s", i+1, r)
	}

	boxDrainer, ok := appInstance.drainer.(*outbox.Drainer)
	require.True(t, ok, "drainer must be *outbox.Drainer")

	drainRes, err := boxDrainer.RunOnce(ctx)
	require.NoError(t, err, "outbox drainer RunOnce failed")
	t.Logf("Drainer RunOnce claimed: %d, done: %d, failed: %d", drainRes.Claimed, drainRes.Done, drainRes.Failed)

	finalEvents, _ := liveGateLoadStream(t, pool, pr.Number)
	finalAnalysis := liveGateAnalyzeEvents(finalEvents, pr)
	require.NotNil(t, finalAnalysis.lastGateEval, "expected gate.evaluated in final stream")

	status, conclusion := liveGateReadCheckPayload(t, pool)

	t.Logf("Final gate.evaluated OK: %t", finalAnalysis.lastGateEval.OK)
	t.Logf("Final gate.evaluated reasons count: %d", len(finalAnalysis.lastGateEval.Reasons))
	for i, reason := range finalAnalysis.lastGateEval.Reasons {
		t.Logf("Final gate reason [%d]: %s", i+1, reason)
	}
	t.Logf("Final posted check status: %s, conclusion: %s", status, conclusion)
	t.Logf("Final recorded approvals: %d", finalAnalysis.numApprovals)
	t.Logf("Final recorded CI results: %d", finalAnalysis.numCIResults)

	require.True(t, finalAnalysis.lastGateEval.OK, "gate evaluated ok must be true; unmet clauses: %v", finalAnalysis.lastGateEval.Reasons)
	require.Empty(t, finalAnalysis.lastGateEval.Reasons, "gate evaluated reasons must be empty")
	require.Equal(t, "completed", status, "posted check status must be completed")
	require.Equal(t, "success", conclusion, "posted check conclusion must be success")
}
