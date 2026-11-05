package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// liveSections is a complete set of the sections a live run needs. Each secret value is a file name.
const liveSections = `
[database]
url_secret = "database_url"

[jira]
base_url = "https://example.atlassian.net"
project_keys = ["KAN"]
email_secret = "jira_email"
api_token_secret = "jira_api_token"

[switch]
base_url = "https://switch.example.test"
agent_id = "agent-1"
agent_name = "heimdall-coordinator"
bridge_id = "bridge-1"
api_key_secret = "switch_api_key"
registration_token_secret = "switch_registration_token"

[github]
app_id_secret = "github_app_id"
installation_id_secret = "github_app_installation_id"
private_key_secret = "github_app_private_key"

[agents]
test_author = "test-author"
reviewers = ["reviewer-1", "reviewer-2"]
resolver = "resolver"
`

func TestLoadReadsLiveSections(t *testing.T) {
	cfg, err := Load(writeTOML(t, "secrets_dir = \""+t.TempDir()+"\"\n"+liveSections))
	require.NoError(t, err)
	require.Equal(t, Database{URLSecret: "database_url"}, cfg.Database)
	require.Equal(t, Jira{
		BaseURL:        "https://example.atlassian.net",
		ProjectKeys:    []string{"KAN"},
		EmailSecret:    "jira_email",
		APITokenSecret: "jira_api_token",
	}, cfg.Jira)
	require.Equal(t, Switch{
		BaseURL:                 "https://switch.example.test",
		AgentID:                 "agent-1",
		AgentName:               "heimdall-coordinator",
		BridgeID:                "bridge-1",
		APIKeySecret:            "switch_api_key",
		RegistrationTokenSecret: "switch_registration_token",
	}, cfg.Switch)
	require.Equal(t, GitHub{
		AppIDSecret:          "github_app_id",
		InstallationIDSecret: "github_app_installation_id",
		PrivateKeySecret:     "github_app_private_key",
	}, cfg.GitHub)
	require.Equal(t, Agents{
		TestAuthor: "test-author",
		Reviewers:  []string{"reviewer-1", "reviewer-2"},
		Resolver:   "resolver",
	}, cfg.Agents)
}

func TestLoadReadsSwitchAgentRuntimeContract(t *testing.T) {
	body := "secrets_dir = \"" + t.TempDir() + "\"\n" + liveSections + `
[agent_runtime]
provider = "switchroom"
machine = "heimdall-vm"
mcp_endpoint = "http://127.0.0.1:8080/mcp"
model = "openrouter/deepseek/deepseek-v4-flash"
`
	cfg, err := Load(writeTOML(t, body))
	require.NoError(t, err)
	provider, found := cfg.Selection("agent_runtime")
	require.True(t, found)
	require.Equal(t, "switchroom", provider)
	require.Equal(t, AgentRuntimeConfig{
		Machine: "heimdall-vm", MCPEndpoint: "http://127.0.0.1:8080/mcp", Model: "openrouter/deepseek/deepseek-v4-flash",
	}, cfg.AgentRuntime)
}

func TestAgentRuntimeRequiresSafeHostReachableMCPEndpoint(t *testing.T) {
	head := "secrets_dir = \"" + t.TempDir() + "\"\n"
	cases := []struct{ endpoint, want string }{
		{"http://heimdall.example.test/mcp", "loopback"},
		{"https://heimdall.example.test/", "/mcp URL"},
		{"https://user@heimdall.example.test/mcp", "/mcp URL"},
	}
	for _, tc := range cases {
		t.Run(tc.endpoint, func(t *testing.T) {
			body := head + liveSections + "\n[agent_runtime]\nprovider=\"switchroom\"\nmachine=\"heimdall-vm\"\nmcp_endpoint=\"" + tc.endpoint + "\"\n"
			_, err := Load(writeTOML(t, body))
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestLoadAcceptsLoopbackHTTPURLs(t *testing.T) {
	cases := map[string]string{
		"ipv4":       "http://127.0.0.1:8081",
		"localhost":  "http://localhost:8081",
		"ipv6":       "http://[::1]:8081",
		"https":      "https://jira.example.test",
		"https port": "https://jira.example.test:8443",
	}
	for name, url := range cases {
		t.Run(name, func(t *testing.T) {
			body := "[jira]\nbase_url = \"" + url + "\"\nproject_keys = [\"KAN\"]\nemail_secret = \"jira_email\"\napi_token_secret = \"jira_api_token\"\n"
			_, err := Load(writeTOML(t, "secrets_dir = \""+t.TempDir()+"\"\n"+body))
			require.NoError(t, err)
		})
	}
}

// TestLoadDoesNotEchoAMistakenURLSecret proves a URL written where a secret file name belongs is refused without
// printing its password, which may be real.
func TestLoadDoesNotEchoAMistakenURLSecret(t *testing.T) {
	head := "secrets_dir = \"" + t.TempDir() + "\"\n"
	_, err := Load(writeTOML(t, head+"[database]\nurl_secret = \"postgres://user:hunter2@db/heimdall\"\n"))
	require.ErrorContains(t, err, "database.url_secret")
	require.NotContains(t, err.Error(), "hunter2")
}

func TestLoadRefusesBadLiveSections(t *testing.T) {
	head := "secrets_dir = \"" + t.TempDir() + "\"\n"
	cases := []struct {
		name    string
		toml    string
		wantErr string
	}{
		{"database section without url_secret", head + "[database]\n", "database.url_secret"},
		{"empty database url_secret", head + "[database]\nurl_secret = \"\"\n", "database.url_secret"},
		{"database url_secret is a URL", head + "[database]\nurl_secret = \"postgres://user:pass@db/heimdall\"\n", "database.url_secret must be a secret file name, not a URL"},
		{"database secret name with a path", head + "[database]\nurl_secret = \"sub/database_url\"\n", "database.url_secret"},
		{"jira section without base_url", head + "[jira]\nproject_keys = [\"KAN\"]\nemail_secret = \"jira_email\"\napi_token_secret = \"jira_api_token\"\n", "jira.base_url"},
		{"jira base_url over plain http off loopback", head + "[jira]\nbase_url = \"http://jira.example.test\"\nproject_keys = [\"KAN\"]\nemail_secret = \"jira_email\"\napi_token_secret = \"jira_api_token\"\n", "jira.base_url"},
		{"jira base_url without a scheme", head + "[jira]\nbase_url = \"jira.example.test\"\nproject_keys = [\"KAN\"]\nemail_secret = \"jira_email\"\napi_token_secret = \"jira_api_token\"\n", "jira.base_url"},
		{"jira base_url with the wrong scheme", head + "[jira]\nbase_url = \"ftp://jira.example.test\"\nproject_keys = [\"KAN\"]\nemail_secret = \"jira_email\"\napi_token_secret = \"jira_api_token\"\n", "jira.base_url"},
		{"jira section without project_keys", head + "[jira]\nbase_url = \"https://jira.example.test\"\nemail_secret = \"jira_email\"\napi_token_secret = \"jira_api_token\"\n", "jira.project_keys"},
		{"empty project_keys", head + "[jira]\nbase_url = \"https://jira.example.test\"\nproject_keys = []\nemail_secret = \"jira_email\"\napi_token_secret = \"jira_api_token\"\n", "jira.project_keys"},
		{"lowercase project key", head + "[jira]\nbase_url = \"https://jira.example.test\"\nproject_keys = [\"kan\"]\nemail_secret = \"jira_email\"\napi_token_secret = \"jira_api_token\"\n", "jira.project_keys"},
		{"duplicate project keys", head + "[jira]\nbase_url = \"https://jira.example.test\"\nproject_keys = [\"KAN\", \"KAN\"]\nemail_secret = \"jira_email\"\napi_token_secret = \"jira_api_token\"\n", "jira.project_keys"},
		{"jira section without email_secret", head + "[jira]\nbase_url = \"https://jira.example.test\"\nproject_keys = [\"KAN\"]\napi_token_secret = \"jira_api_token\"\n", "jira.email_secret"},
		{"jira section without api_token_secret", head + "[jira]\nbase_url = \"https://jira.example.test\"\nproject_keys = [\"KAN\"]\nemail_secret = \"jira_email\"\n", "jira.api_token_secret"},
		{"jira secret name with a path", head + "[jira]\nbase_url = \"https://jira.example.test\"\nproject_keys = [\"KAN\"]\nemail_secret = \"sub/jira_email\"\napi_token_secret = \"jira_api_token\"\n", "jira.email_secret"},
		{"switch section without base_url", head + "[switch]\nagent_id = \"a\"\nagent_name = \"n\"\nbridge_id = \"b\"\napi_key_secret = \"switch_api_key\"\nregistration_token_secret = \"switch_registration_token\"\n", "switch.base_url"},
		{"switch base_url over plain http off loopback", head + "[switch]\nbase_url = \"http://switch.example.test\"\nagent_id = \"a\"\nagent_name = \"n\"\nbridge_id = \"b\"\napi_key_secret = \"switch_api_key\"\nregistration_token_secret = \"switch_registration_token\"\n", "switch.base_url"},
		{"switch section without agent_id", head + "[switch]\nbase_url = \"https://switch.example.test\"\nagent_name = \"n\"\nbridge_id = \"b\"\napi_key_secret = \"switch_api_key\"\nregistration_token_secret = \"switch_registration_token\"\n", "switch.agent_id"},
		{"switch section without agent_name", head + "[switch]\nbase_url = \"https://switch.example.test\"\nagent_id = \"a\"\nbridge_id = \"b\"\napi_key_secret = \"switch_api_key\"\nregistration_token_secret = \"switch_registration_token\"\n", "switch.agent_name"},
		{"switch section without bridge_id", head + "[switch]\nbase_url = \"https://switch.example.test\"\nagent_id = \"a\"\nagent_name = \"n\"\napi_key_secret = \"switch_api_key\"\nregistration_token_secret = \"switch_registration_token\"\n", "switch.bridge_id"},
		{"switch section without api_key_secret", head + "[switch]\nbase_url = \"https://switch.example.test\"\nagent_id = \"a\"\nagent_name = \"n\"\nbridge_id = \"b\"\nregistration_token_secret = \"switch_registration_token\"\n", "switch.api_key_secret"},
		{"switch section without registration_token_secret", head + "[switch]\nbase_url = \"https://switch.example.test\"\nagent_id = \"a\"\nagent_name = \"n\"\nbridge_id = \"b\"\napi_key_secret = \"switch_api_key\"\n", "switch.registration_token_secret"},
		{"github section without app_id_secret", head + "[github]\ninstallation_id_secret = \"github_app_installation_id\"\nprivate_key_secret = \"github_app_private_key\"\n", "github.app_id_secret"},
		{"github section without installation_id_secret", head + "[github]\napp_id_secret = \"github_app_id\"\nprivate_key_secret = \"github_app_private_key\"\n", "github.installation_id_secret"},
		{"github section without private_key_secret", head + "[github]\napp_id_secret = \"github_app_id\"\ninstallation_id_secret = \"github_app_installation_id\"\n", "github.private_key_secret"},
		{"github secret name with a path", head + "[github]\napp_id_secret = \"github_app_id\"\ninstallation_id_secret = \"github_app_installation_id\"\nprivate_key_secret = \"sub/key\"\n", "github.private_key_secret"},
		{"unknown database key", head + "[database]\nurl_secret = \"database_url\"\nurl = \"postgres://x\"\n", "unknown key"},
		{"unknown jira key", head + "[jira]\nbase_url = \"https://jira.example.test\"\nproject_keys = [\"KAN\"]\nemail_secret = \"jira_email\"\napi_token_secret = \"jira_api_token\"\nproject = \"KAN\"\n", "unknown key"},
		{"unknown switch key", head + "[switch]\nbase_url = \"https://switch.example.test\"\nagent_id = \"a\"\nagent_name = \"n\"\nbridge_id = \"b\"\napi_key_secret = \"switch_api_key\"\nregistration_token_secret = \"switch_registration_token\"\nagent = \"a\"\n", "unknown key"},
		{"unknown github key", head + "[github]\napp_id_secret = \"github_app_id\"\ninstallation_id_secret = \"github_app_installation_id\"\nprivate_key_secret = \"github_app_private_key\"\nkey = \"x\"\n", "unknown key"},
		{"agents section without test_author", head + "[agents]\nreviewers = [\"rev1\"]\nresolver = \"res1\"\n", "agents.test_author"},
		{"agents section with bad test_author", head + "[agents]\ntest_author = \"Bad Author!\"\nreviewers = [\"rev1\"]\nresolver = \"res1\"\n", "agents.test_author"},
		{"agents section without reviewers", head + "[agents]\ntest_author = \"author\"\nresolver = \"res1\"\n", "agents.reviewers"},
		{"agents section with empty reviewers", head + "[agents]\ntest_author = \"author\"\nreviewers = []\nresolver = \"res1\"\n", "agents.reviewers"},
		{"agents section with duplicate reviewers", head + "[agents]\ntest_author = \"author\"\nreviewers = [\"rev1\", \"rev1\"]\nresolver = \"res1\"\n", "agents.reviewers"},
		{"agents section with bad reviewer name", head + "[agents]\ntest_author = \"author\"\nreviewers = [\"bad reviewer!\"]\nresolver = \"res1\"\n", "agents.reviewers"},
		{"agents section without resolver", head + "[agents]\ntest_author = \"author\"\nreviewers = [\"rev1\"]\n", "agents.resolver"},
		{"agents section with bad resolver", head + "[agents]\ntest_author = \"author\"\nreviewers = [\"rev1\"]\nresolver = \"bad resolver!\"\n", "agents.resolver"},
		{"unknown agents key", head + "[agents]\ntest_author = \"author\"\nreviewers = [\"rev1\"]\nresolver = \"res1\"\nextra = \"bad\"\n", "unknown key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(writeTOML(t, tc.toml))
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestLoadReportsEveryBadLiveKeyAtOnce(t *testing.T) {
	body := "[database]\nurl_secret = \"\"\n" +
		"[jira]\nbase_url = \"http://jira.example.test\"\nproject_keys = [\"kan\"]\nemail_secret = \"\"\napi_token_secret = \"\"\n" +
		"[switch]\nbase_url = \"http://switch.example.test\"\nagent_id = \"\"\nagent_name = \"\"\nbridge_id = \"\"\napi_key_secret = \"\"\nregistration_token_secret = \"\"\n" +
		"[github]\napp_id_secret = \"\"\ninstallation_id_secret = \"\"\nprivate_key_secret = \"\"\n"
	_, err := Load(writeTOML(t, "secrets_dir = \""+t.TempDir()+"\"\n"+body))
	for _, key := range []string{
		"database.url_secret", "jira.base_url", "jira.project_keys", "jira.email_secret", "jira.api_token_secret",
		"switch.base_url", "switch.agent_id", "switch.agent_name", "switch.bridge_id", "switch.api_key_secret",
		"switch.registration_token_secret", "github.app_id_secret", "github.installation_id_secret", "github.private_key_secret",
	} {
		require.ErrorContains(t, err, key)
	}
}
