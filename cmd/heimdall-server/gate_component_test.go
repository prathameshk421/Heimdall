//go:build component

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/policyyaml"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/postgres"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/ids"
	testpg "github.com/prathameshk421/flip_the_switch_hackathon/internal/testkit/postgres"
)

const (
	gateTestRepo     = "acme/pay"
	gateTestChange   = id.ChangeID("PAY-1")
	gateTestBaseSHA  = id.SHA("b100000000000000000000000000000000000001")
	gateTestHead1    = id.SHA("a100000000000000000000000000000000000001")
	gateTestHead2    = id.SHA("a200000000000000000000000000000000000002")
	gateTestTreeHash = id.SHA("c100000000000000000000000000000000000001")
	gateTestAuthor   = "author1"
	gateTestPollTick = 20 * time.Millisecond
	gateTestWaitFor  = 10 * time.Second
)

const gateTestPolicyYAML = `version: 1
workflow: default
approvers: { default: [bob], payments: [bob] }
policy_owners: [bob]
people:
  bob: { github: bob, chat: dev-bob, teams: [payments] }
domains: { "pkg/**": payments }
reviewers: { count: 3, pick_timeout: 15m }
ci_required: [build]
github: { mode: poll, interval: 10s }
slop: { max_changed_lines: 400, require_ticket: true }
tests:
  dir: tests/heimdall
  allowed_imports: [github.com/stretchr/testify]
  min_mutation_score: 0.6
blocking: { min_severity: medium, min_tier: E3 }
consensus: { min_high_reporters: 2, block_score: 1.5, min_reporters: majority, confidence: { high: 1.0, medium: 0.5, low: 0.2 }, weight: { start: 0.7, min: 0.3, max: 1.0, min_decided: 5 } }
deliberation: { max_rounds: 2, human_only_domains: [money_movement, data_deletion] }
keys: { openrouter: production }
models: { provider: openrouter, test_author: openrouter/deepseek/deepseek-v4-flash, reviewer: openrouter/deepseek/deepseek-v4-flash, resolver: openrouter/deepseek/deepseek-v4-flash, extraction: deepseek/deepseek-v4-flash, embedding: "openrouter:mongodb/voyage-4-lite" }
budgets: { test_author: 400000, reviewer: 150000, resolver: 120000, per_change: 1200000 }
skills: { test_author: "1.0.0", reviewer: "1.0.0", resolver: "1.0.0" }
memory: { rooms: [payments-eng], chat_keywords: [decided, decision, "we will", incident, postmortem] }
pack: { max_age_days: 14 }
timeouts: { question: 4h, remind_after: 2h, escalate_after: 8h, reject_after: 24h }
chat: { bridge: mattermost, verbosity: milestones, decisions_in_room: true }
tracker: { kind: jira, mode: poll, interval: 30s }
agents: { runtime: opencode }
`

type recordedCheckCall struct {
	Repo       string
	SHA        string
	Name       string
	Status     string
	Conclusion string
	ExternalID string
	Summary    string
}

type fakeGateGitHub struct {
	srv         *httptest.Server
	policyBytes []byte
	mu          sync.Mutex
	headSHA     id.SHA
	reviews     []map[string]any
	checks      []recordedCheckCall
	pullsCalls  int
	checkIDSeq  int64
}

func newFakeGateGitHub(t *testing.T, policyBytes []byte) *fakeGateGitHub {
	t.Helper()
	f := &fakeGateGitHub{
		policyBytes: policyBytes,
		headSHA:     gateTestHead1,
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGateGitHub) setHeadSHA(sha id.SHA) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.headSHA = sha
}

func (f *fakeGateGitHub) setReviews(revs []map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reviews = append([]map[string]any(nil), revs...)
}

func (f *fakeGateGitHub) checkCallsCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.checks)
}

func (f *fakeGateGitHub) lastGateCheck() (recordedCheckCall, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.checks) - 1; i >= 0; i-- {
		if f.checks[i].Name == "heimdall/gate" {
			return f.checks[i], true
		}
	}
	return recordedCheckCall{}, false
}

func (f *fakeGateGitHub) pullsCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pullsCalls
}

func (f *fakeGateGitHub) serve(w http.ResponseWriter, r *http.Request) {
	repoPath := "/repos/" + gateTestRepo
	if f.serveAuth(w, r) ||
		f.serveRepo(w, r, repoPath) ||
		f.servePulls(w, r, repoPath) ||
		f.serveCheckRuns(w, r, repoPath) ||
		f.serveGit(w, r, repoPath) ||
		f.serveContents(w, r, repoPath) {
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found: " + r.URL.Path})
}

func (f *fakeGateGitHub) serveAuth(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodPost && r.URL.Path == fmt.Sprintf("/app/installations/%s/access_tokens", acceptanceInstall) {
		writeJSON(w, http.StatusCreated, map[string]any{
			"token":      acceptanceTicketTok,
			"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		})
		return true
	}
	return false
}

func (f *fakeGateGitHub) serveRepo(w http.ResponseWriter, r *http.Request, repoPath string) bool {
	if r.Method != http.MethodGet {
		return false
	}
	switch r.URL.Path {
	case repoPath:
		writeJSON(w, http.StatusOK, map[string]any{"default_branch": "main"})
		return true
	case repoPath + "/commits/main":
		writeJSON(w, http.StatusOK, map[string]any{"sha": string(gateTestBaseSHA)})
		return true
	}
	return false
}

func (f *fakeGateGitHub) servePulls(w http.ResponseWriter, r *http.Request, repoPath string) bool {
	if r.Method != http.MethodGet {
		return false
	}
	switch r.URL.Path {
	case repoPath + "/pulls":
		f.mu.Lock()
		f.pullsCalls++
		head := f.headSHA
		f.mu.Unlock()
		writeJSON(w, http.StatusOK, []any{f.prJSON(head)})
		return true
	case repoPath + "/pulls/1":
		f.mu.Lock()
		head := f.headSHA
		f.mu.Unlock()
		writeJSON(w, http.StatusOK, f.prJSON(head))
		return true
	case repoPath + "/pulls/1/files":
		writeJSON(w, http.StatusOK, []any{
			map[string]any{
				"filename":  "pkg/pay/pay.go",
				"status":    "added",
				"additions": 10,
				"deletions": 0,
			},
		})
		return true
	case repoPath + "/pulls/1/reviews":
		f.mu.Lock()
		revs := append([]map[string]any(nil), f.reviews...)
		f.mu.Unlock()
		writeJSON(w, http.StatusOK, revs)
		return true
	}
	return false
}

func (f *fakeGateGitHub) prJSON(head id.SHA) map[string]any {
	return map[string]any{
		"number": 1,
		"title":  "PAY-1: Add payment feature",
		"body":   "## Justification\nInitial implementation of PAY-1.",
		"state":  "open",
		"merged": false,
		"user":   map[string]any{"login": gateTestAuthor},
		"head":   map[string]any{"ref": "feature/PAY-1", "sha": string(head)},
		"base":   map[string]any{"sha": string(gateTestBaseSHA)},
	}
}

func (f *fakeGateGitHub) serveCheckRuns(w http.ResponseWriter, r *http.Request, repoPath string) bool {
	switch {
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, repoPath+"/commits/") && strings.HasSuffix(r.URL.Path, "/check-runs"):
		parts := strings.Split(r.URL.Path, "/")
		sha := parts[5]
		if r.URL.Query().Get("check_name") == "heimdall/gate" {
			f.serveExistingGateChecks(w, sha)
			return true
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"total_count": 2,
			"check_runs": []any{
				map[string]any{"id": 201, "name": "build", "head_sha": sha, "status": "completed", "conclusion": "success"},
				map[string]any{"id": 202, "name": "heimdall/acceptance", "head_sha": sha, "status": "completed", "conclusion": "success"},
			},
		})
		return true
	case r.Method == http.MethodPost && r.URL.Path == repoPath+"/check-runs":
		f.serveCreateCheckRun(w, r)
		return true
	case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, repoPath+"/check-runs/"):
		f.servePatchCheckRun(w, r)
		return true
	}
	return false
}

func (f *fakeGateGitHub) serveExistingGateChecks(w http.ResponseWriter, sha string) {
	f.mu.Lock()
	var existing []any
	for _, c := range f.checks {
		if c.SHA == sha && c.Name == "heimdall/gate" {
			existing = append(existing, map[string]any{
				"id":          1001,
				"name":        c.Name,
				"head_sha":    c.SHA,
				"status":      c.Status,
				"conclusion":  c.Conclusion,
				"external_id": c.ExternalID,
			})
		}
	}
	f.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"total_count": len(existing), "check_runs": existing})
}

func (f *fakeGateGitHub) serveCreateCheckRun(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name       string `json:"name"`
		HeadSHA    string `json:"head_sha"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		ExternalID string `json:"external_id"`
		Output     struct {
			Title   string `json:"title"`
			Summary string `json:"summary"`
		} `json:"output"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "bad body"})
		return
	}
	f.mu.Lock()
	f.checkIDSeq++
	idVal := f.checkIDSeq
	f.checks = append(f.checks, recordedCheckCall{
		Repo:       gateTestRepo,
		SHA:        body.HeadSHA,
		Name:       body.Name,
		Status:     body.Status,
		Conclusion: body.Conclusion,
		ExternalID: body.ExternalID,
		Summary:    body.Output.Summary,
	})
	f.mu.Unlock()
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":          idVal,
		"name":        body.Name,
		"head_sha":    body.HeadSHA,
		"status":      body.Status,
		"conclusion":  body.Conclusion,
		"external_id": body.ExternalID,
	})
}

func (f *fakeGateGitHub) servePatchCheckRun(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		ExternalID string `json:"external_id"`
		Output     struct {
			Title   string `json:"title"`
			Summary string `json:"summary"`
		} `json:"output"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.mu.Lock()
	f.checkIDSeq++
	f.checks = append(f.checks, recordedCheckCall{
		Repo:       gateTestRepo,
		SHA:        string(f.headSHA),
		Name:       "heimdall/gate",
		Status:     body.Status,
		Conclusion: body.Conclusion,
		ExternalID: body.ExternalID,
		Summary:    body.Output.Summary,
	})
	f.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"id":          1001,
		"status":      body.Status,
		"conclusion":  body.Conclusion,
		"external_id": body.ExternalID,
	})
}

func (f *fakeGateGitHub) serveContents(w http.ResponseWriter, r *http.Request, repoPath string) bool {
	if r.Method == http.MethodGet && r.URL.Path == repoPath+"/contents/heimdall.yaml" {
		w.Header().Set("Content-Type", "application/vnd.github.raw+json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(f.policyBytes)
		return true
	}
	return false
}

func (f *fakeGateGitHub) serveGit(w http.ResponseWriter, r *http.Request, repoPath string) bool {
	if r.Method != http.MethodGet {
		return false
	}
	switch {
	case strings.HasPrefix(r.URL.Path, repoPath+"/git/commits/"):
		sha := strings.TrimPrefix(r.URL.Path, repoPath+"/git/commits/")
		writeJSON(w, http.StatusOK, map[string]any{"sha": sha, "tree": map[string]any{"sha": "tree-" + sha}})
		return true
	case strings.HasPrefix(r.URL.Path, repoPath+"/git/trees/"):
		treeSHA := strings.TrimPrefix(r.URL.Path, repoPath+"/git/trees/")
		f.serveTree(w, treeSHA)
		return true
	}
	return false
}

func (f *fakeGateGitHub) serveTree(w http.ResponseWriter, treeSHA string) {
	switch treeSHA {
	case "tree-tests":
		writeJSON(w, http.StatusOK, map[string]any{
			"sha":  treeSHA,
			"tree": []any{map[string]any{"path": "heimdall", "type": "tree", "sha": "tree-heimdall"}},
		})
	case "tree-heimdall":
		writeJSON(w, http.StatusOK, map[string]any{
			"sha":  treeSHA,
			"tree": []any{map[string]any{"path": "PAY-1", "type": "tree", "sha": string(gateTestTreeHash)}},
		})
	default:
		if strings.HasPrefix(treeSHA, "tree-") {
			writeJSON(w, http.StatusOK, map[string]any{
				"sha":  treeSHA,
				"tree": []any{map[string]any{"path": "tests", "type": "tree", "sha": "tree-tests"}},
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"sha": treeSHA, "tree": []any{}})
	}
}

func writeGateAcceptanceConfig(t *testing.T, secretsDir string) string {
	t.Helper()
	body := fmt.Sprintf(`listen = "127.0.0.1:0"
secrets_dir = %q

[database]
url_secret = "database_url"

[jira]
base_url = "https://jira.example.test"
project_keys = ["PAY"]
email_secret = "jira_email"
api_token_secret = "jira_api_token"

[switch]
base_url = "https://switch.example.test"
agent_id = %q
agent_name = %q
bridge_id = %q
api_key_secret = "switch_api_key"
registration_token_secret = "switch_registration_token"

[github]
app_id_secret = "github_app_id"
installation_id_secret = "github_app_installation_id"
private_key_secret = "github_app_private_key"

[vcs]
provider = "github"

[tracker]
provider = "jira"

[chat]
bridge = "switch"

[agents]
test_author = "test-author"
reviewers = ["reviewer-1", "reviewer-2"]
resolver = "resolver"

[[projects]]
tracker_project = "PAY"
repo = %q
base_branch = "main"
`, secretsDir, acceptanceAgentID, acceptanceAgentName, acceptanceBridgeID, gateTestRepo)
	path := filepath.Join(t.TempDir(), "heimdall.toml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func seedPinnedChange(t *testing.T, store *postgres.Store) {
	t.Helper()
	now := clock.System{}.Now().UTC()
	gen := ids.New()

	mkEvent := func(v int64, et change.EventType, actor change.Actor, p change.Payload) change.Event {
		raw, err := json.Marshal(p)
		require.NoError(t, err)
		eid := gen.NewEvent()
		ev := change.Event{
			ID:            eid,
			ChangeID:      gateTestChange,
			Version:       v,
			Type:          et,
			Actor:         actor,
			OccurredAt:    now,
			CorrelationID: change.CorrelationID(eid.String()),
			Payload:       raw,
		}
		require.NoError(t, ev.Validate())
		return ev
	}

	taskID := gen.NewTask()
	runID := gen.NewRun()
	evs := []change.Event{
		mkEvent(1, change.EventChangeTriggered, change.Actor{Kind: change.ActorJira, ID: "jira"},
			change.Triggered{Ticket: gateTestChange, Repo: change.Repo(gateTestRepo), Assignee: gateTestAuthor, Source: change.TriggerJira}),
		mkEvent(2, change.EventRoomOpened, change.Actor{Kind: change.ActorSystem, ID: "switch"},
			change.RoomOpened{RoomID: "room-1"}),
		mkEvent(3, change.EventTaskAssigned, change.Actor{Kind: change.ActorSystem, ID: "engine"},
			change.TaskAssigned{TaskID: taskID, Agent: "tester", Role: workflow.RoleTestAuthor, Deadline: now.Add(time.Hour), BundleRef: workflow.BlobRef("blob:sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")}),
		mkEvent(4, change.EventTestsSubmitted, change.Actor{Kind: change.ActorHuman, ID: gateTestAuthor},
			change.TestsSubmitted{Refs: []string{"AC1"}, ACMap: map[string][]string{"AC1": {"tests/heimdall/PAY-1/test.go"}}, SHA: gateTestHead1, TreeHash: gateTestTreeHash}),
		mkEvent(5, change.EventTestsVerified, change.Actor{Kind: change.ActorSystem, ID: "tester"},
			change.TestsVerified{RunIDs: []id.RunID{runID}, SHA: gateTestHead1, TreeHash: gateTestTreeHash, NeedsSignoff: false}),
		mkEvent(6, change.EventTestsCommitted, change.Actor{Kind: change.ActorSystem, ID: "engine"},
			change.TestsCommitted{SHA: gateTestHead1, TreeHash: gateTestTreeHash}),
	}

	_, err := store.Append(context.Background(), gateTestChange, 0, evs, nil)
	require.NoError(t, err)
}

func TestMergeGateEndToEnd(t *testing.T) {
	// Parse and validate the policy YAML using real policyyaml decoder
	pol, err := policyyaml.Decode([]byte(gateTestPolicyYAML))
	require.NoError(t, err, "policy YAML must decode cleanly")
	require.Equal(t, "tests/heimdall", pol.Tests.Dir)
	require.Equal(t, []string{"build"}, pol.CIRequired)

	dsn := testpg.Start(t)
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	require.NoError(t, postgres.Migrate(context.Background(), pool))

	store := postgres.NewStore(pool)
	// Seed the change stream directly through event store before sync
	seedPinnedChange(t, store)

	jira := newFakeJira(t, time.Now().Add(-time.Minute))
	sw := newFakeSwitch(t)
	gh := newFakeGateGitHub(t, []byte(gateTestPolicyYAML))

	secretsDir := writeAcceptanceSecrets(t)
	cfgPath := writeGateAcceptanceConfig(t, secretsDir)

	startApp := func() func() {
		ctx := context.Background()
		cfg, err := config.Load(cfgPath)
		require.NoError(t, err)
		d := deps{
			databaseURL: dsn,
			endpoints: endpoints{
				jiraBaseURL:     jira.srv.URL,
				githubAPI:       gh.srv.URL,
				switchBaseURL:   sw.srv.URL,
				switchAgentID:   acceptanceAgentID,
				switchAgentName: acceptanceAgentName,
				switchBridgeID:  acceptanceBridgeID,
			},
			httpClient: &http.Client{},
			logger:     slog.New(slog.NewTextHandler(os.Stderr, nil)),
			pollEvery:  gateTestPollTick,
			syncEvery:  gateTestPollTick,
			drainTick:  gateTestPollTick,
			openPool:   pgxpool.New,
			migrate:    postgres.Migrate,
		}
		a, err := build(ctx, cfg, d)
		require.NoError(t, err)
		stop := startLoops(ctx, a)
		return func() {
			stop()
			a.close()
		}
	}

	stop := startApp()

	// (a) After the first sync the fake check writer receives a heimdall/gate check on H1 that is NOT a success
	// (reasons name the missing approval).
	require.Eventually(t, func() bool {
		c, ok := gh.lastGateCheck()
		if !ok || c.SHA != string(gateTestHead1) {
			return false
		}
		return c.Conclusion != "success" &&
			strings.Contains(c.Summary, "approval: no approval has been recorded from an approver of the changed domains")
	}, gateTestWaitFor, gateTestPollTick, "(a) check on H1 must name missing approval")

	// (b) After the fake GitHub shows an APPROVED review by bob on H1:
	// Approval is satisfied; missing approval reason is cleared.
	// (Note: full success is blocked because reviewers are out of scope and review requests are unknown).
	gh.setReviews([]map[string]any{
		{
			"id":           101,
			"state":        "APPROVED",
			"commit_id":    string(gateTestHead1),
			"submitted_at": time.Now().UTC().Format(time.RFC3339),
			"user":         map[string]any{"login": "bob"},
		},
	})
	require.Eventually(t, func() bool {
		c, ok := gh.lastGateCheck()
		if !ok || c.SHA != string(gateTestHead1) {
			return false
		}
		return !strings.Contains(c.Summary, "approval: no approval has been recorded")
	}, gateTestWaitFor, gateTestPollTick, "(b) approval by bob on H1 must clear missing approval reason")

	// (c) After the fake GitHub moves the PR head to H2 (a push), the next check on H2 is not a success again
	// (the approval no longer counts) and the old approval is not counted.
	gh.setHeadSHA(gateTestHead2)
	require.Eventually(t, func() bool {
		c, ok := gh.lastGateCheck()
		if !ok || c.SHA != string(gateTestHead2) {
			return false
		}
		return c.Conclusion != "success" &&
			strings.Contains(c.Summary, "approval: no approval has been recorded from an approver of the changed domains")
	}, gateTestWaitFor, gateTestPollTick, "(c) check on H2 must name missing approval again")

	// (d) The same review turning DISMISSED (same review id, new state) makes approval impossible.
	gh.setHeadSHA(gateTestHead1)
	gh.setReviews([]map[string]any{
		{
			"id":           101,
			"state":        "DISMISSED",
			"commit_id":    string(gateTestHead1),
			"submitted_at": time.Now().UTC().Format(time.RFC3339),
			"user":         map[string]any{"login": "bob"},
		},
	})
	require.Eventually(t, func() bool {
		c, ok := gh.lastGateCheck()
		if !ok || c.SHA != string(gateTestHead1) {
			return false
		}
		return c.Conclusion != "success" &&
			strings.Contains(c.Summary, "approval: no approval has been recorded from an approver of the changed domains")
	}, gateTestWaitFor, gateTestPollTick, "(d) dismissed review must invalidate approval on H1")

	// (e) A restart of the app on the same database does not post a duplicate check for an unchanged verdict.
	callsBefore := gh.checkCallsCount()
	stop()

	pullsBefore := gh.pullsCount()
	stop = startApp()
	// Wait until restarted app has executed at least one sync pass
	require.Eventually(t, func() bool {
		return gh.pullsCount() > pullsBefore
	}, gateTestWaitFor, gateTestPollTick, "restarted app must sync")

	callsAfter := gh.checkCallsCount()
	require.Equal(t, callsBefore, callsAfter, "(e) restart on unchanged verdict must not post duplicate checks")
	stop()
}
