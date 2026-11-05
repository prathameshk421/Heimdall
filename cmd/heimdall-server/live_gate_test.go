//go:build live

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/github"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/postgres"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/outbox"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/ids"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	testpg "github.com/prathameshk421/flip_the_switch_hackathon/internal/testkit/postgres"
)

// The live gate tests talk to the real GitHub API as the Heimdall GitHub App on the real
// fixture pull request aarush323/heimdall-fixture-pay #1. They are not part of make fast.
//
// Credentials:
// Each test reads credentials from the repository's .env file at runtime:
//   - GITHUB_APP_ID: integer GitHub App ID
//   - GITHUB_APP_INSTALLATION_ID: integer installation ID
//   - GITHUB_APP_PRIVATE_KEY_PATH: path to the App's private key PEM file
// If any of these values are missing or the key file cannot be read, the test skips with a clear message.
// Nothing from the credential file or environment is printed or put in an error.
//
// What these tests write to GitHub:
// Exactly one check run named "heimdall/gate" on the head commit of PR #1 in aarush323/heimdall-fixture-pay
// (via GitHub Checks API). No other files, comments, or branches are modified.
//
// How to run:
//
//   TestLiveGateOnFixturePR:
//     go test -tags live -count=1 -run TestLiveGateOnFixturePR -v ./cmd/heimdall-server/
//
//   TestLiveGateAfterApproval:
//     First approve PR #1 with a non-author GitHub account:
//       gh pr review 1 --approve -R aarush323/heimdall-fixture-pay
//     Then run:
//       go test -tags live -count=1 -run TestLiveGateAfterApproval -v ./cmd/heimdall-server/

const liveGateFixtureRepo = change.Repo("aarush323/heimdall-fixture-pay")

type liveFakeTickets struct{}

func (liveFakeTickets) Changes(context.Context, ports.Cursor) ([]ports.TicketChange, ports.Cursor, error) {
	return nil, "", nil
}

func (liveFakeTickets) Ticket(context.Context, id.ChangeID) (ports.Ticket, error) {
	return ports.Ticket{}, nil
}

type liveFakeChat struct{}

func (liveFakeChat) OpenRoom(context.Context, ports.RoomRequest) (change.RoomID, error) {
	return "", nil
}

func (liveFakeChat) Post(context.Context, change.RoomID, change.EffectKey, string) error {
	return nil
}

func (liveFakeChat) Read(context.Context, change.RoomID, ports.Cursor) ([]ports.Message, ports.Cursor, error) {
	return nil, "", nil
}

func liveGateRepoRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "../.."
	}
	for i := 0; i < 5; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	return "../.."
}

func liveGateReadDotEnv(t *testing.T, path string) map[string]string {
	t.Helper()
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	return out
}

func liveGateAdapter(t *testing.T) *github.Adapter {
	t.Helper()
	root := liveGateRepoRoot()
	env := liveGateReadDotEnv(t, filepath.Join(root, ".env"))
	appID, err := strconv.ParseInt(env["GITHUB_APP_ID"], 10, 64)
	if err != nil {
		t.Skip("GITHUB_APP_ID is missing from .env: live test skipped")
	}
	instID, err := strconv.ParseInt(env["GITHUB_APP_INSTALLATION_ID"], 10, 64)
	if err != nil {
		t.Skip("GITHUB_APP_INSTALLATION_ID is missing from .env: live test skipped")
	}
	keyPath := env["GITHUB_APP_PRIVATE_KEY_PATH"]
	if keyPath == "" {
		t.Skip("GITHUB_APP_PRIVATE_KEY_PATH is missing from .env: live test skipped")
	}
	if !filepath.IsAbs(keyPath) {
		keyPath = filepath.Join(root, keyPath)
	}
	pemBytes, err := os.ReadFile(keyPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("GitHub App private key file %s does not exist: live test skipped", keyPath)
		}
		t.Fatalf("read App key file: %v", err)
	}
	a, err := github.New(github.Config{
		BaseURL:    "https://api.github.com",
		App:        github.App{AppID: appID, InstallationID: instID, PrivateKeyPEM: pemBytes},
		HTTPClient: &http.Client{Timeout: 40 * time.Second},
		Clock:      clock.System{},
		Wait: func(ctx context.Context, d time.Duration) error {
			select {
			case <-time.After(d):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	})
	if err != nil {
		t.Fatalf("github.New: %v", err)
	}
	return a
}

func liveGateFindPR1(t *testing.T, ghAdapter *github.Adapter) ports.PR {
	t.Helper()
	ctx := context.Background()
	openPRs, err := ghAdapter.OpenPRs(ctx, liveGateFixtureRepo)
	require.NoError(t, err, "OpenPRs on fixture repo")
	require.NotEmpty(t, openPRs, "fixture repository %s has no open pull requests", liveGateFixtureRepo)

	for _, p := range openPRs {
		if p.Number == 1 {
			return p
		}
	}
	t.Fatalf("PR #1 not found in open pull requests on %s", liveGateFixtureRepo)
	return ports.PR{}
}

func liveGateBuildApp(t *testing.T, ghAdapter *github.Adapter) (*app, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	dsn := testpg.Start(t)
	d := deps{
		databaseURL: dsn,
		endpoints: endpoints{
			jiraBaseURL:   "https://jira.example.test",
			githubAPI:     githubAPIRoot,
			switchBaseURL: "https://switch.example.test",
		},
		httpClient: &http.Client{Timeout: 40 * time.Second},
		logger:     slog.Default(),
		pollEvery:  pollInterval,
		syncEvery:  syncInterval,
		drainTick:  drainTick,
		openPool:   pgxpool.New,
		migrate:    postgres.Migrate,
	}

	pool, err := d.openPool(ctx, d.databaseURL)
	require.NoError(t, err, "open database pool")

	ad := adapters{
		tickets:  liveFakeTickets{},
		vcs:      ghAdapter,
		chat:     liveFakeChat{},
		projects: projectRepos{"PAY": liveGateFixtureRepo},
		project:  "PAY",
		repos:    []change.Repo{liveGateFixtureRepo},
	}

	appInstance, err := assemble(ctx, d, ad, pool, clock.System{})
	if err != nil {
		pool.Close()
		require.NoError(t, err, "assemble app")
	}
	t.Cleanup(appInstance.close)
	return appInstance, pool
}

func liveGateLoadStream(t *testing.T, pool *pgxpool.Pool, prNumber int) ([]change.Event, id.ChangeID) {
	t.Helper()
	ctx := context.Background()
	store := postgres.NewStore(pool)

	var changeIDs []id.ChangeID
	rows, err := pool.Query(ctx, "SELECT DISTINCT change_id FROM event")
	require.NoError(t, err, "query distinct change_id from event table")
	for rows.Next() {
		var cid string
		require.NoError(t, rows.Scan(&cid))
		changeIDs = append(changeIDs, id.ChangeID(cid))
	}
	rows.Close()
	require.NoError(t, rows.Err(), "iterate change_id rows")
	require.NotEmpty(t, changeIDs, "no change stream found in store after sync")

	for _, cid := range changeIDs {
		evs, err := store.Load(ctx, cid)
		require.NoError(t, err, "load change stream %s", cid)
		for _, e := range evs {
			if e.Type == change.EventPROpened {
				var po change.PROpened
				if err := json.Unmarshal(e.Payload, &po); err == nil && po.Number == prNumber {
					return evs, cid
				}
			}
		}
	}

	first := changeIDs[0]
	evs, err := store.Load(ctx, first)
	require.NoError(t, err, "load change stream %s", first)
	return evs, first
}

type eventAnalysis struct {
	lastGateEval        *change.GateEvaluated
	numApprovals        int
	numCIResults        int
	approvedByNonAuthor bool
	approverLogin       change.ActorID
}

func liveGateAnalyzeEvents(events []change.Event, pr ports.PR) eventAnalysis {
	var res eventAnalysis
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == change.EventGateEvaluated && res.lastGateEval == nil {
			var ge change.GateEvaluated
			if err := json.Unmarshal(events[i].Payload, &ge); err == nil {
				res.lastGateEval = &ge
			}
		}
	}

	for _, e := range events {
		if e.Type == change.EventApprovalRecorded {
			res.numApprovals++
			var ar change.ApprovalRecorded
			if err := json.Unmarshal(e.Payload, &ar); err == nil {
				if ar.CommitSHA == pr.HeadSHA && ar.Login != pr.Author {
					res.approvedByNonAuthor = true
					res.approverLogin = ar.Login
				}
			}
		}
		if e.Type == change.EventCIReported {
			res.numCIResults++
		}
	}
	return res
}

func liveGateReadCheckPayload(t *testing.T, pool *pgxpool.Pool) (string, string) {
	t.Helper()
	ctx := context.Background()
	var payloadBytes []byte
	err := pool.QueryRow(ctx, "SELECT payload FROM outbox WHERE kind = $1 ORDER BY id DESC LIMIT 1", string(change.EffectGitHubCheck)).Scan(&payloadBytes)
	require.NoError(t, err, "read github_check effect payload from outbox")

	var checkPayload struct {
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		Summary    string `json:"summary"`
	}
	require.NoError(t, json.Unmarshal(payloadBytes, &checkPayload), "unmarshal check payload")
	return checkPayload.Status, checkPayload.Conclusion
}

// liveGateSeedTriggered starts the change the pull request belongs to, as the Jira trigger would. Intake refuses a PR
// that names a ticket no change exists for, and this test has no tracker. The ticket key comes from the PR title.
func liveGateSeedTriggered(t *testing.T, pool *pgxpool.Pool, pr ports.PR) {
	t.Helper()
	key := id.ChangeID(strings.SplitN(pr.Title, ":", 2)[0])
	pl := change.Triggered{Ticket: key, Repo: liveGateFixtureRepo, Assignee: "prathameshk421", Source: change.TriggerManual}
	require.NoError(t, pl.Validate())
	raw, err := json.Marshal(pl)
	require.NoError(t, err)
	gen := ids.New()
	eid := gen.NewEvent()
	ev := change.Event{
		ID: eid, ChangeID: key, Version: 1, Type: change.EventChangeTriggered,
		Actor:         change.Actor{Kind: change.ActorSystem, ID: "engine"},
		OccurredAt:    time.Now().UTC(),
		CorrelationID: change.CorrelationID(eid.String()),
		Payload:       raw,
	}
	require.NoError(t, ev.Validate())
	_, err = postgres.NewStore(pool).Append(context.Background(), key, 0, []change.Event{ev}, nil)
	require.NoError(t, err, "seed change.triggered for %s", key)
}

func runLiveGateTest(t *testing.T, checkApproval bool) {
	t.Helper()
	ghAdapter := liveGateAdapter(t)
	pr := liveGateFindPR1(t, ghAdapter)

	appInstance, pool := liveGateBuildApp(t, ghAdapter)
	ctx := context.Background()
	liveGateSeedTriggered(t, pool, pr)

	syncRes, err := appInstance.syncer.SyncOnce(ctx)
	require.NoError(t, err, "syncer.SyncOnce failed")
	require.Empty(t, syncRes.Failed, "PR sync failed for: %v", syncRes.Failed)
	require.Positive(t, syncRes.Synced, "expected at least one PR to be synced")

	boxDrainer, ok := appInstance.drainer.(*outbox.Drainer)
	require.True(t, ok, "drainer must be *outbox.Drainer")

	drainRes, err := boxDrainer.RunOnce(ctx)
	require.NoError(t, err, "outbox drainer RunOnce failed")
	require.Positive(t, drainRes.Claimed, "expected at least one effect to be claimed")
	var rawPayload []byte
	if err := pool.QueryRow(ctx, "SELECT payload FROM outbox WHERE kind = $1 ORDER BY id DESC LIMIT 1", string(change.EffectGitHubCheck)).Scan(&rawPayload); err == nil {
		t.Logf("github_check payload as stored: %s", rawPayload) // repo, sha, name, status, summary: no credentials
	}
	require.Positive(t, drainRes.Done, "expected at least one effect to be executed")
	require.Zero(t, drainRes.Failed, "expected no effect to fail execution")

	events, streamID := liveGateLoadStream(t, pool, pr.Number)
	require.NotEmpty(t, events, "target change stream must not be empty")

	analysis := liveGateAnalyzeEvents(events, pr)
	status, conclusion := liveGateReadCheckPayload(t, pool)

	t.Logf("PR number: #%d", pr.Number)
	t.Logf("head SHA: %s", pr.HeadSHA)
	if analysis.lastGateEval == nil {
		// A PR that fails the slop rule is rejected at the door: nothing else runs, so there is no gate.evaluated.
		t.Logf("no gate.evaluated event in stream %s: the slop rule decided the verdict", streamID)
	} else {
		t.Logf("gate.evaluated ok: %t", analysis.lastGateEval.OK)
		t.Logf("gate.evaluated reasons count: %d", len(analysis.lastGateEval.Reasons))
		for i, reason := range analysis.lastGateEval.Reasons {
			t.Logf("gate reason [%d]: %s", i+1, reason)
		}
	}
	t.Logf("recorded approvals: %d", analysis.numApprovals)
	t.Logf("recorded CI results: %d", analysis.numCIResults)
	t.Logf("posted check conclusion: %q (status: %s)", conclusion, status)

	if checkApproval {
		if analysis.approvedByNonAuthor {
			t.Logf("approval of PR head %s by non-author login recorded: true (by %s)", pr.HeadSHA.Short(), analysis.approverLogin)
		} else {
			t.Logf("approval of PR head %s by non-author login recorded: false", pr.HeadSHA.Short())
		}
	}
}

func TestLiveGateOnFixturePR(t *testing.T) {
	runLiveGateTest(t, false)
}

func TestLiveGateAfterApproval(t *testing.T) {
	runLiveGateTest(t, true)
}
