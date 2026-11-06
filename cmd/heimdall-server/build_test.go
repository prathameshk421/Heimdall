package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/postgres"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/effects"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/ids"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const testDatabaseURL = "postgres://heimdall:heimdall@127.0.0.1:1/heimdall?sslmode=disable"

const trackerProjectBody = `
[[projects]]
tracker_project = "KAN"
repo = "aarush323/PBL_TOS_RISK_ANALYZER"
base_branch = "heimdall-demo"
`

// loadTestConfig writes a heimdall.toml with the given selections and loads it through config.Load.
func loadTestConfig(t *testing.T, body string) config.Config {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "heimdall.toml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	cfg, err := config.Load(path)
	require.NoError(t, err)
	return cfg
}

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

// testDeps is a valid deps value that never reaches the database when a config is refused first.
func testDeps() deps {
	return deps{
		databaseURL: testDatabaseURL,
		endpoints:   endpoints{jiraBaseURL: "http://127.0.0.1:1", githubAPI: "http://127.0.0.1:1", switchBaseURL: "http://127.0.0.1:1"},
		httpClient:  &http.Client{},
		logger:      discardLogger(),
		pollEvery:   time.Second,
		syncEvery:   time.Second,
		drainTick:   time.Second,
		openPool:    pgxpool.New,
		migrate:     postgres.Migrate,
	}
}

func TestBuildRefusesAMissingProviderAndNamesIt(t *testing.T) {
	cfg := loadTestConfig(t, `
[vcs]
provider = "github"

[chat]
bridge = "switch"
`+trackerProjectBody)
	_, err := build(context.Background(), cfg, testDeps())
	require.ErrorContains(t, err, "tracker.provider")
	require.NotContains(t, err.Error(), "vcs.provider", "a selected provider is not named as missing")
}

func TestBuildNamesEveryMissingProvider(t *testing.T) {
	cfg := loadTestConfig(t, trackerProjectBody)
	_, err := build(context.Background(), cfg, testDeps())
	require.ErrorContains(t, err, "vcs.provider")
	require.ErrorContains(t, err, "tracker.provider")
	require.ErrorContains(t, err, "chat.bridge")
}

func TestBuildRefusesAnUnknownProviderAndListsTheValidNames(t *testing.T) {
	cfg := loadTestConfig(t, `
[vcs]
provider = "github"

[tracker]
provider = "linear"

[chat]
bridge = "switch"
`+trackerProjectBody)
	_, err := build(context.Background(), cfg, testDeps())
	require.ErrorContains(t, err, "tracker.provider")
	require.ErrorContains(t, err, `"linear"`)
	require.ErrorContains(t, err, "valid names are jira")
}

func TestBuildRefusesMoreThanOneTrackerProject(t *testing.T) {
	cfg := loadTestConfig(t, `
[vcs]
provider = "github"

[tracker]
provider = "jira"

[chat]
bridge = "switch"

[[projects]]
tracker_project = "KAN"
repo = "aarush323/PBL_TOS_RISK_ANALYZER"
base_branch = "heimdall-demo"

[[projects]]
tracker_project = "PAY"
repo = "aarush323/heimdall-fixture-pay"
base_branch = "main"
`)
	_, err := build(context.Background(), cfg, testDeps())
	require.ErrorContains(t, err, "maps 2")
}

func TestBuildRefusesDepsThatCannotRun(t *testing.T) {
	cfg := loadTestConfig(t, trackerProjectBody)
	d := testDeps()
	d.pollEvery = 0
	_, err := build(context.Background(), cfg, d)
	require.ErrorContains(t, err, "poll interval must be positive")
}

func TestBuildRefusesNonPositiveSyncInterval(t *testing.T) {
	cfg := loadTestConfig(t, trackerProjectBody)
	d := testDeps()
	d.syncEvery = 0
	_, err := build(context.Background(), cfg, d)
	require.ErrorContains(t, err, "sync interval must be positive")
}

func TestProductionDepsNamesTheConfigKeysItLacks(t *testing.T) {
	cfg := loadTestConfig(t, trackerProjectBody)
	_, err := productionDeps(cfg, discardLogger())
	require.ErrorContains(t, err, "database.url_secret")
	require.ErrorContains(t, err, "jira.base_url")
	require.ErrorContains(t, err, "switch.agent_id")
	require.ErrorContains(t, err, "heimdall.toml")
}

type dummyTickets struct{}

func (dummyTickets) Changes(context.Context, ports.Cursor) ([]ports.TicketChange, ports.Cursor, error) {
	return nil, "", nil
}

func (dummyTickets) Ticket(context.Context, id.ChangeID) (ports.Ticket, error) {
	return ports.Ticket{}, nil
}

type dummyChat struct{}

func (dummyChat) OpenRoom(context.Context, ports.RoomRequest) (change.RoomID, error) {
	return "", nil
}

func (dummyChat) Post(context.Context, change.RoomID, change.EffectKey, string) error {
	return nil
}

func (dummyChat) Read(context.Context, change.RoomID, ports.Cursor) ([]ports.Message, ports.Cursor, error) {
	return nil, "", nil
}

type dummyVCS struct{}

func (dummyVCS) EnsureBranch(context.Context, change.Repo, id.SHA, string) error { return nil }
func (dummyVCS) CommitFiles(context.Context, change.Repo, string, string, []ports.File) (id.SHA, error) {
	return "", nil
}
func (dummyVCS) DefaultHead(context.Context, change.Repo) (id.SHA, error) { return "", nil }
func (dummyVCS) PR(context.Context, change.Repo, int) (ports.PR, error)   { return ports.PR{}, nil }
func (dummyVCS) Files(context.Context, change.Repo, int) ([]ports.ChangedFile, error) {
	return nil, nil
}
func (dummyVCS) OpenPRs(context.Context, change.Repo) ([]ports.PR, error) { return nil, nil }
func (dummyVCS) ReadFile(context.Context, change.Repo, id.SHA, string) ([]byte, error) {
	return nil, nil
}
func (dummyVCS) TreeHash(context.Context, change.Repo, id.SHA, string) (id.SHA, error) {
	return "", nil
}
func (dummyVCS) Tarball(context.Context, change.Repo, id.SHA) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}
func (dummyVCS) PRReviews(context.Context, change.Repo, int) ([]ports.PRReview, error) {
	return nil, nil
}
func (dummyVCS) CheckRuns(context.Context, change.Repo, id.SHA) ([]ports.CheckRun, error) {
	return nil, nil
}
func (dummyVCS) SetCheck(context.Context, change.Repo, id.SHA, ports.Check) error { return nil }
func (dummyVCS) Comment(context.Context, change.Repo, int, change.EffectKey, string) error {
	return nil
}

func (dummyVCS) IssueCommentsSince(context.Context, change.Repo, int, time.Time) ([]ports.IssueComment, error) {
	return nil, nil
}

func TestExecutorsRegistersGitHubCheck(t *testing.T) {
	store := newMemStore()
	clk := clock.System{}
	gen := ids.New()
	eng := newSliceEngine(t, store)
	ad := adapters{
		tickets: dummyTickets{},
		vcs:     dummyVCS{},
		chat:    dummyChat{},
	}
	blobs := newMemBlobStore()
	execs, err := executors(context.Background(), ad, store, nil, blobs, ingestor{eng: eng}, gen, clk)
	require.NoError(t, err)
	ch, ok := execs[change.EffectGitHubCheck]
	require.True(t, ok, "github_check executor must be registered")
	require.IsType(t, effects.GitHubCheck{}, ch)
}

func TestTestQualityWiringUsesTargetModuleSeam(t *testing.T) {
	require.Equal(t, []string{"github.com/prathameshk421/flip_the_switch_hackathon/internal"}, testQualityForbiddenImports())
}

func TestNeedsTestSignoffMatchesExpectedPathsAgainstBasePolicy(t *testing.T) {
	pol := policy.Policy{Tests: policy.Tests{Sensitive: []string{"backend/**/auth*", "*delete*"}}}
	require.False(t, needsTestSignoff(pol, []string{"internal/refunds/refunds.go"}))
	require.True(t, needsTestSignoff(pol, []string{"backend/api/authz.go"}))
	require.True(t, needsTestSignoff(pol, []string{"db/delete_user.go"}))
}
