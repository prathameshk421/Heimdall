package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/postgres"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
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

func TestProductionDepsNamesTheConfigKeysItLacks(t *testing.T) {
	cfg := loadTestConfig(t, trackerProjectBody)
	_, err := productionDeps(cfg, discardLogger())
	require.ErrorContains(t, err, "database.url_secret")
	require.ErrorContains(t, err, "jira.base_url")
	require.ErrorContains(t, err, "switch.agent_id")
	require.ErrorContains(t, err, "heimdall.toml")
}
