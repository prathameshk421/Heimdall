package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
)

// defaultSecretNames are the file names the adapters assume when nothing else is configured. They are the
// names deploy/heimdall.example.toml documents.
var defaultSecretNames = map[string]string{
	"database_url":               "database_url",
	"jira_email":                 "jira_email",
	"jira_api_token":             "jira_api_token",
	"github_app_id":              "github_app_id",
	"github_app_installation_id": "github_app_installation_id",
	"github_app_private_key":     "github_app_private_key",
	"switch_api_key":             "switch_api_key",
	"switch_registration_token":  "switch_registration_token",
}

const (
	jiraTokenCanary     = "jira-token-CANARY-9f3"
	switchKeyCanary     = "switch-key-CANARY-4a1"
	switchRegCanary     = "switch-registration-CANARY-2c7"
	badDatabaseValue    = "not-a-postgres-url-CANARY-5d2"
	agentAuthorToken    = "agent-author-CANARY-7e1"
	agentReviewer1Token = "agent-reviewer1-CANARY-8f2"
	agentReviewer2Token = "agent-reviewer2-CANARY-9a3"
	agentResolverToken  = "agent-resolver-CANARY-2b4"
)

// writeLiveSecrets writes one file per credential and returns their directory. The private key is a real
// 2048-bit RSA key in PEM form, mode 0600, as the config requires for a multi-line secret.
func writeLiveSecrets(t *testing.T, names map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	values := map[string]string{
		"database_url":               testDatabaseURL,
		"jira_email":                 "heimdall-bot@example.test",
		"jira_api_token":             jiraTokenCanary,
		"github_app_id":              "7",
		"github_app_installation_id": "42",
		"github_app_private_key":     string(keyPEM),
		"switch_api_key":             switchKeyCanary,
		"switch_registration_token":  switchRegCanary,
		"agent_token_test-author":    agentAuthorToken,
		"agent_token_reviewer-1":     agentReviewer1Token,
		"agent_token_reviewer-2":     agentReviewer2Token,
		"agent_token_resolver":       agentResolverToken,
	}
	for role, name := range names {
		value, ok := values[role]
		require.True(t, ok, "unknown secret role %q", role)
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(value+"\n"), 0o600))
	}
	// Agent tokens are always needed by the MCP edge; their file names are fixed by the agents below.
	for _, name := range []string{"agent_token_test-author", "agent_token_reviewer-1", "agent_token_reviewer-2", "agent_token_resolver"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(values[name]+"\n"), 0o600))
	}
	return dir
}

// liveConfigBody builds a complete heimdall.toml. The URLs point at test doubles; nothing is called at build
// time. Each secret key names a file in secretsDir (the map lets a test choose non-default names).
func liveConfigBody(secretsDir, jiraURL string, names map[string]string) string {
	return fmt.Sprintf(`listen = "127.0.0.1:0"
secrets_dir = %q

[database]
url_secret = %q

[jira]
base_url = %q
project_keys = ["KAN"]
email_secret = %q
api_token_secret = %q

[switch]
base_url = "https://switch.example.test"
agent_id = "agent-coord-1"
agent_name = "heimdall-coordinator"
bridge_id = "bridge-1"
api_key_secret = %q
registration_token_secret = %q

[github]
app_id_secret = %q
installation_id_secret = %q
private_key_secret = %q

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
tracker_project = "KAN"
repo = "aarush323/PBL_TOS_RISK_ANALYZER"
base_branch = "heimdall-demo"
`, secretsDir,
		names["database_url"], jiraURL, names["jira_email"], names["jira_api_token"],
		names["switch_api_key"], names["switch_registration_token"],
		names["github_app_id"], names["github_app_installation_id"], names["github_app_private_key"])
}

func liveConfig(t *testing.T, names map[string]string) (cfg config.Config, dir string) {
	t.Helper()
	dir = writeLiveSecrets(t, names)
	cfg = loadTestConfig(t, liveConfigBody(dir, "https://jira.example.test", names))
	return cfg, dir
}

func TestProductionDepsReadsACompleteConfig(t *testing.T) {
	cfg, _ := liveConfig(t, defaultSecretNames)
	d, err := productionDeps(cfg, discardLogger())
	require.NoError(t, err)
	require.Equal(t, testDatabaseURL, d.databaseURL, "the database URL is read through the secret name")
	require.Equal(t, "https://jira.example.test", d.endpoints.jiraBaseURL)
	require.Equal(t, "https://switch.example.test", d.endpoints.switchBaseURL)
	require.Equal(t, "agent-coord-1", d.endpoints.switchAgentID)
	require.Equal(t, "heimdall-coordinator", d.endpoints.switchAgentName)
	require.Equal(t, "bridge-1", d.endpoints.switchBridgeID)
	require.Equal(t, githubAPIRoot, d.endpoints.githubAPI)
	require.NotNil(t, d.httpClient)
	require.NotNil(t, d.logger)
	require.Positive(t, d.pollEvery)
	require.Positive(t, d.drainTick)
	require.NoError(t, d.validate())
}

func TestProductionDepsNamesEveryMissingKey(t *testing.T) {
	cfg := loadTestConfig(t, trackerProjectBody)
	_, err := productionDeps(cfg, discardLogger())
	require.ErrorContains(t, err, "heimdall.toml")
	for _, key := range []string{
		"database.url_secret",
		"tracker.provider",
		"switch.base_url", "switch.agent_id", "switch.agent_name", "switch.bridge_id",
		"switch.api_key_secret", "switch.registration_token_secret",
		"github.app_id_secret", "github.installation_id_secret", "github.private_key_secret",
		"agents.test_author", "agents.reviewers", "agents.resolver",
	} {
		require.ErrorContains(t, err, key)
	}
}

func TestProductionDepsRefusesAWorldReadablePrivateKey(t *testing.T) {
	cfg, dir := liveConfig(t, defaultSecretNames)
	keyName := defaultSecretNames["github_app_private_key"]
	require.NoError(t, os.Chmod(filepath.Join(dir, keyName), 0o644))
	_, err := productionDeps(cfg, discardLogger())
	require.ErrorContains(t, err, keyName)
	require.ErrorContains(t, err, "mode")
	for _, canary := range []string{"BEGIN RSA PRIVATE KEY", jiraTokenCanary, switchKeyCanary, switchRegCanary} {
		require.NotContains(t, err.Error(), canary, "an error must not carry a secret value")
	}
}

func TestProductionDepsRefusesAnUnparseableDatabaseURLWithoutLeakingIt(t *testing.T) {
	cfg, dir := liveConfig(t, defaultSecretNames)
	require.NoError(t, os.WriteFile(filepath.Join(dir, defaultSecretNames["database_url"]), []byte(badDatabaseValue+"\n"), 0o600))
	_, err := productionDeps(cfg, discardLogger())
	require.ErrorContains(t, err, "database.url_secret")
	require.NotContains(t, err.Error(), badDatabaseValue, "the database URL is a secret and must not be in the error")
}

func TestCompleteConfigSelectsEveryAdapter(t *testing.T) {
	cfg, _ := liveConfig(t, defaultSecretNames)
	d, err := productionDeps(cfg, discardLogger())
	require.NoError(t, err)
	ad, err := selectAdapters(cfg, d)
	require.NoError(t, err, "a complete config must build every adapter")
	require.NotNil(t, ad.tickets)
	require.NotNil(t, ad.vcs)
	require.NotNil(t, ad.chat)
	require.Equal(t, "KAN", ad.project)
}

func TestFileTrackerConfigBuildsWithoutJiraSettingsOrSecrets(t *testing.T) {
	tickets := t.TempDir()
	names := map[string]string{
		"database_url": "database_url", "github_app_id": "github_app_id",
		"github_app_installation_id": "github_app_installation_id", "github_app_private_key": "github_app_private_key",
		"switch_api_key": "switch_api_key", "switch_registration_token": "switch_registration_token",
	}
	secrets := writeLiveSecrets(t, names)
	body := liveConfigBody(secrets, "", names)
	jiraSection := "[jira]\nbase_url = \"\"\nproject_keys = [\"KAN\"]\nemail_secret = \"\"\napi_token_secret = \"\"\n\n"
	body = strings.Replace(body, jiraSection, "", 1)
	body = strings.Replace(body, "provider = \"jira\"", "provider = \"file\"\npath = \""+tickets+"\"", 1)
	cfg := loadTestConfig(t, body)
	d, err := productionDeps(cfg, discardLogger())
	require.NoError(t, err)
	ad, err := selectAdapters(cfg, d)
	require.NoError(t, err)
	require.NotNil(t, ad.tickets)
	require.Equal(t, "file", ad.trackerProvider)
}

func TestCompleteConfigBuildsAnAppWithFakes(t *testing.T) {
	cfg, _ := liveConfig(t, defaultSecretNames)
	d, err := productionDeps(cfg, discardLogger())
	require.NoError(t, err)
	// The opener is the lazy pgxpool (it does not connect until first use) and the migration is a fake, so this
	// proves the config, the adapters and the loop wiring without a live database.
	d.openPool = pgxpool.New
	d.migrate = func(context.Context, *pgxpool.Pool) error { return nil }
	a, err := build(context.Background(), cfg, d)
	require.NoError(t, err)
	require.NotNil(t, a)
	require.NotNil(t, a.poller)
	require.NotNil(t, a.drainer)
	a.close()
}

func TestCompleteConfigUsesTheConfiguredSecretNames(t *testing.T) {
	custom := map[string]string{
		"database_url":               "live_database_url",
		"jira_email":                 "live_jira_email",
		"jira_api_token":             "live_jira_token",
		"github_app_id":              "live_github_app_id",
		"github_app_installation_id": "live_github_installation_id",
		"github_app_private_key":     "live_github_key",
		"switch_api_key":             "live_switch_key",
		"switch_registration_token":  "live_switch_registration",
	}
	cfg, _ := liveConfig(t, custom)
	d, err := productionDeps(cfg, discardLogger())
	require.NoError(t, err)
	_, err = selectAdapters(cfg, d)
	require.NoError(t, err, "the adapters must read the secret file names the config gives, not the defaults")
}

func TestProductionDepsRefusesProjectKeysThatDoNotMatchTheProjectMap(t *testing.T) {
	cfg, _ := liveConfig(t, defaultSecretNames)
	_, err := productionDeps(cfg, discardLogger())
	require.NoError(t, err, "the helper config maps KAN and lists KAN")

	body := liveConfigBody(writeLiveSecrets(t, defaultSecretNames), "https://jira.example.test", defaultSecretNames)
	body = strings.Replace(body, `tracker_project = "KAN"`, `tracker_project = "PAY"`, 1)
	cfg = loadTestConfig(t, body)
	_, err = productionDeps(cfg, discardLogger())
	require.ErrorContains(t, err, "jira.project_keys")
	require.ErrorContains(t, err, "KAN")
	require.ErrorContains(t, err, "PAY")
}

func TestDepsRedactsDatabaseURLInFormattingAndLogging(t *testing.T) {
	cfg, _ := liveConfig(t, defaultSecretNames)
	d, err := productionDeps(cfg, discardLogger())
	require.NoError(t, err)

	for name, formatted := range map[string]string{
		"String":   d.String(),
		"GoString": d.GoString(),
		"%v":       fmt.Sprintf("%v", d),
		"%+v":      fmt.Sprintf("%+v", d),
		"%#v":      fmt.Sprintf("%#v", d),
	} {
		t.Run(name, func(t *testing.T) {
			require.NotContains(t, formatted, testDatabaseURL, "formatting must never leak the database URL")
			require.Contains(t, formatted, "[redacted]", "formatting must show the redacted placeholder")
		})
	}

	t.Run("slog", func(t *testing.T) {
		val := d.LogValue()
		group := val.Group()
		var found bool
		for _, attr := range group {
			if attr.Key == "databaseURL" {
				found = true
				require.Equal(t, "[redacted]", attr.Value.String())
			}
		}
		require.True(t, found, "LogValue must include databaseURL attribute")
	})
}

func TestBuildDoesNotLeakDatabaseURLOnOpenPoolFailure(t *testing.T) {
	cfg, _ := liveConfig(t, defaultSecretNames)
	d, err := productionDeps(cfg, discardLogger())
	require.NoError(t, err)
	d.openPool = func(_ context.Context, url string) (*pgxpool.Pool, error) {
		return nil, fmt.Errorf("failed to connect to %s: auth failed", url)
	}
	_, err = build(context.Background(), cfg, d)
	require.ErrorContains(t, err, "open database")
	require.NotContains(t, err.Error(), testDatabaseURL)
	require.Contains(t, err.Error(), "[redacted]")
}

func TestProductionDepsRefusesMultipleProjectKeysOrMultipleProjects(t *testing.T) {
	// Multiple project keys
	body := liveConfigBody(writeLiveSecrets(t, defaultSecretNames), "https://jira.example.test", defaultSecretNames)
	bodyMultiKey := strings.Replace(body, `project_keys = ["KAN"]`, `project_keys = ["KAN", "PAY"]`, 1)
	cfgMultiKey := loadTestConfig(t, bodyMultiKey)
	_, err := productionDeps(cfgMultiKey, discardLogger())
	require.ErrorContains(t, err, "jira.project_keys")
	require.ErrorContains(t, err, "lists 2")

	// Multiple projects in mapping
	bodyMultiProj := body + "\n[[projects]]\ntracker_project = \"PAY\"\nrepo = \"owner/pay\"\nbase_branch = \"main\"\n"
	cfgMultiProj := loadTestConfig(t, bodyMultiProj)
	_, err = productionDeps(cfgMultiProj, discardLogger())
	require.ErrorContains(t, err, "projects")
	require.ErrorContains(t, err, "maps 2")
}
