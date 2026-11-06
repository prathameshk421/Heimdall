package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/registry"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/stretchr/testify/require"
)

// plantedConnectSecret is written into secret files in these tests. No output, plan, JSON or error may contain it.
const plantedConnectSecret = "PLANTED-CONNECT-VALUE-41c2e9"

// connectFixture writes heimdall.toml in a temp dir. Its secrets_dir is dir/secrets, which is created, and
// each entry of files is written there with mode 0600. extra is appended to the TOML.
func connectFixture(t *testing.T, extra string, files map[string]string) (dir, cfgPath string) {
	t.Helper()
	dir = t.TempDir()
	secretsDir := filepath.Join(dir, "secrets")
	require.NoError(t, os.Mkdir(secretsDir, 0o700))
	cfgPath = filepath.Join(dir, "heimdall.toml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("secrets_dir = \""+secretsDir+"\"\n"+extra), 0o600))
	for name, value := range files {
		require.NoError(t, os.WriteFile(filepath.Join(secretsDir, name), []byte(value+"\n"), 0o600))
	}
	return dir, cfgPath
}

func testConnectDeps() connectDeps {
	return connectDeps{
		getenv:        func(string) string { return "" },
		loadConfig:    config.Load,
		registry:      defaultRegistry,
		checkDeadline: 10 * time.Second,
	}
}

func mustLoad(t *testing.T, path string) config.Config {
	t.Helper()
	cfg, err := config.Load(path)
	require.NoError(t, err)
	return cfg
}

func TestDefaultRegistryDescribesPlannedEntries(t *testing.T) {
	reg, err := defaultRegistry()
	require.NoError(t, err)
	require.Equal(t, []registry.Description{
		{Name: "github", Kind: registry.KindVCS, RequiredConfig: []string{"github_app_id", "github_app_installation_id", "github_app_private_key"}},
		{Name: "file", Kind: registry.KindTracker},
		{Name: "jira", Kind: registry.KindTracker, RequiredConfig: []string{"jira_api_token", "jira_email"}},
		{Name: "mattermost", Kind: registry.KindChat, RequiredConfig: []string{"mattermost_token"}},
		{Name: "switch", Kind: registry.KindChat, RequiredConfig: []string{"switch_api_key", "switch_registration_token"}},
		{Name: "teams", Kind: registry.KindChat},
		{Name: "opencode", Kind: registry.KindAgentRuntime, RequiredConfig: []string{"openrouter_api_key"}},
		{Name: "graphiti", Kind: registry.KindMemory},
	}, reg.Describe())
}

func TestDefaultRegistryChecksReportNeedsConfigAndNeverConnected(t *testing.T) {
	cases := []struct {
		name       string
		kind       registry.Kind
		files      map[string]string
		wantDetail string
	}{
		{"github", registry.KindVCS, nil, "github_app_id"},
		{"jira", registry.KindTracker, nil, "jira.base_url"},
		{"switch", registry.KindChat, nil, "Switch coordinator configuration"},
		{"mattermost", registry.KindChat, nil, "mattermost_token"},
		{"teams", registry.KindChat, nil, "adapter not built yet"},
		{"file", registry.KindTracker, nil, "adapter not built yet"},
		{"github", registry.KindVCS, map[string]string{
			"github_app_id": "1", "github_app_installation_id": "2", "github_app_private_key": plantedConnectSecret,
		}, "PEM format"},
		{"jira", registry.KindTracker, map[string]string{"jira_email": "person", "jira_api_token": plantedConnectSecret}, "jira.base_url"},
	}
	reg, err := defaultRegistry()
	require.NoError(t, err)
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s/%v", tc.name, tc.files != nil), func(t *testing.T) {
			_, cfgPath := connectFixture(t, "", tc.files)
			cfg := mustLoad(t, cfgPath)
			e, err := reg.Select(tc.kind, tc.name)
			require.NoError(t, err)
			r := e.Check(context.Background(), cfg)
			require.Equal(t, registry.StatusNeedsConfig, r.Status)
			require.Contains(t, r.Detail, tc.wantDetail)
			require.NotContains(t, r.Detail, plantedConnectSecret)
		})
	}
}

func TestCheckWithDeadlineReportsUnreachableWhenCheckHangs(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	e := registry.Entry{
		Name:    "slow",
		Kind:    registry.KindVCS,
		Factory: func(config.Config) (any, error) { return nil, nil },
		Check: func(context.Context, config.Config) registry.CheckResult {
			<-block
			return registry.CheckResult{Status: registry.StatusConnected, Detail: "late"}
		},
	}
	r := checkWithDeadline(context.Background(), e, config.Config{}, 50*time.Millisecond)
	require.Equal(t, registry.StatusUnreachable, r.Status)
	require.Contains(t, r.Detail, "did not answer")
}

func TestCheckWithDeadlinePassesDeadlineAndKeepsAnswer(t *testing.T) {
	var sawDeadline bool
	e := registry.Entry{
		Name:    "quick",
		Kind:    registry.KindVCS,
		Factory: func(config.Config) (any, error) { return nil, nil },
		Check: func(ctx context.Context, _ config.Config) registry.CheckResult {
			_, sawDeadline = ctx.Deadline()
			return registry.CheckResult{Status: registry.StatusNeedsConfig, Detail: "answered"}
		},
	}
	r := checkWithDeadline(context.Background(), e, config.Config{}, time.Second)
	require.True(t, sawDeadline, "the check must receive a context with a deadline")
	require.Equal(t, registry.CheckResult{Status: registry.StatusNeedsConfig, Detail: "answered"}, r)
}

func TestConnectListPrintsEveryEntryWithSelection(t *testing.T) {
	_, cfgPath := connectFixture(t, "[tracker]\nprovider = \"jira\"\n", map[string]string{"jira_api_token": "x"})
	var out, errOut bytes.Buffer
	code := runConnect([]string{"--config", cfgPath, "list"}, strings.NewReader(""), &out, &errOut, testConnectDeps())
	require.Equal(t, 0, code, errOut.String())
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Len(t, lines, 9, out.String())
	require.Equal(t, []string{"NAME", "KIND", "SELECTED", "STATUS"}, strings.Fields(lines[0])[:4])
	rows := map[string][]string{}
	for _, line := range lines[1:] {
		f := strings.Fields(line)
		rows[f[0]] = f[:4]
	}
	require.Equal(t, []string{"jira", "tracker", "yes", "needs_config"}, rows["jira"])
	require.Equal(t, []string{"file", "tracker", "no", "needs_config"}, rows["file"])
	require.Equal(t, []string{"github", "vcs", "no", "needs_config"}, rows["github"])
}

func TestConnectCheckIsNonInteractiveAndChecksEveryRegisteredIntegration(t *testing.T) {
	_, cfgPath := connectFixture(t, "", nil)
	var out, errOut bytes.Buffer
	code := runConnect([]string{"--check", "--config", cfgPath}, strings.NewReader("must-not-prompt"), &out, &errOut, testConnectDeps())

	require.Equal(t, 0, code, errOut.String())
	require.Contains(t, out.String(), "github")
	require.Contains(t, out.String(), "jira")
	require.Contains(t, out.String(), "opencode")
	require.Contains(t, out.String(), "graphiti")
	require.Empty(t, errOut.String())
}

func TestConnectPlansContainResumableCoreIntegrationHandoffs(t *testing.T) {
	_, cfgPath := connectFixture(t, "", nil)
	cfg := mustLoad(t, cfgPath)
	reg, err := defaultRegistry()
	require.NoError(t, err)
	cases := []struct {
		kind registry.Kind
		name string
		want string
	}{
		{registry.KindVCS, "github", "approve and install the GitHub App from its manifest"},
		{registry.KindTracker, "jira", "Jira site URL and project key"},
		{registry.KindChat, "switch", "coordinator identity"},
		{registry.KindAgentRuntime, "opencode", "OpenCode 1.19.5"},
		{registry.KindMemory, "graphiti", "Compose waits for Graphiti's /health check"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := PlanConnect(reg, cfg, tc.kind, tc.name, map[string]string{})
			require.NoError(t, err)
			require.Contains(t, strings.Join(plan.Instructions, "\n"), tc.want)
			require.Contains(t, strings.Join(plan.Instructions, "\n"), "heimdall connect --check")
		})
	}
}

func TestConnectListFlagMatchesListSubcommand(t *testing.T) {
	_, cfgPath := connectFixture(t, "", nil)
	var out, errOut bytes.Buffer
	code := runConnect([]string{"--list", "--config", cfgPath}, strings.NewReader(""), &out, &errOut, testConnectDeps())

	require.Equal(t, 0, code, errOut.String())
	require.Contains(t, out.String(), "NAME")
	require.Contains(t, out.String(), "mattermost")
}

func TestConnectListJSONCarriesStatusAndRequiredNames(t *testing.T) {
	_, cfgPath := connectFixture(t, "[tracker]\nprovider = \"jira\"\n", nil)
	var out, errOut bytes.Buffer
	code := runConnect([]string{"list", "--json", "--config", cfgPath}, strings.NewReader(""), &out, &errOut, testConnectDeps())
	require.Equal(t, 0, code, errOut.String())
	var rows []connectRow
	require.NoError(t, json.Unmarshal(out.Bytes(), &rows))
	require.Len(t, rows, 8)
	byName := map[string]connectRow{}
	for _, r := range rows {
		byName[r.Name] = r
	}
	require.True(t, byName["jira"].Selected)
	require.False(t, byName["file"].Selected)
	require.Equal(t, "needs_config", byName["jira"].Status)
	require.Equal(t, []string{"jira_api_token", "jira_email"}, byName["jira"].RequiredConfig)
	require.Equal(t, "chat", byName["teams"].Kind)
	require.Contains(t, byName["teams"].Detail, "heimdall connect chat teams")
}

func TestConnectListNeverPrintsSecretValues(t *testing.T) {
	files := map[string]string{
		"github_app_id":              plantedConnectSecret,
		"github_app_installation_id": plantedConnectSecret,
		"github_app_private_key":     plantedConnectSecret,
		"jira_api_token":             plantedConnectSecret,
		"mattermost_token":           plantedConnectSecret,
	}
	_, cfgPath := connectFixture(t, "[tracker]\nprovider = \"jira\"\n", files)
	for _, args := range [][]string{{"list"}, {"list", "--json"}} {
		var out, errOut bytes.Buffer
		code := runConnect(append(args, "--config", cfgPath), strings.NewReader(""), &out, &errOut, testConnectDeps())
		require.Equal(t, 0, code, errOut.String())
		require.NotContains(t, out.String(), plantedConnectSecret)
		require.NotContains(t, errOut.String(), plantedConnectSecret)
	}
}

func TestConnectListRefusesSelectionNotRegistered(t *testing.T) {
	_, cfgPath := connectFixture(t, "[tracker]\nprovider = \"jra\"\n", nil)
	var out, errOut bytes.Buffer
	code := runConnect([]string{"list", "--config", cfgPath}, strings.NewReader(""), &out, &errOut, testConnectDeps())
	require.Equal(t, 1, code)
	require.Contains(t, errOut.String(), "valid names are file, jira")
}

func TestPlanConnectGitHubNamesSecretFilesAndSelection(t *testing.T) {
	dir, cfgPath := connectFixture(t, "", nil)
	cfg := mustLoad(t, cfgPath)
	reg, err := defaultRegistry()
	require.NoError(t, err)
	plan, err := PlanConnect(reg, cfg, registry.KindVCS, "github", nil)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"vcs.provider": "github"}, plan.Selection)
	require.Len(t, plan.SecretFiles, 3)
	names := []string{}
	for _, sf := range plan.SecretFiles {
		names = append(names, sf.Name)
		require.Equal(t, filepath.Join(dir, "secrets", sf.Name), sf.Path)
		require.False(t, sf.Present)
		require.NotEmpty(t, sf.Problem)
	}
	sort.Strings(names)
	require.Equal(t, []string{"github_app_id", "github_app_installation_id", "github_app_private_key"}, names)
	require.NotEmpty(t, plan.Instructions)
	require.Contains(t, strings.Join(plan.Instructions, "\n"), filepath.Join(dir, "secrets", "github_app_private_key"))
}

func TestPlanConnectMarksPresentSecretFile(t *testing.T) {
	_, cfgPath := connectFixture(t, "", map[string]string{"jira_email": "operator@example.invalid", "jira_api_token": "value"})
	cfg := mustLoad(t, cfgPath)
	reg, err := defaultRegistry()
	require.NoError(t, err)
	plan, err := PlanConnect(reg, cfg, registry.KindTracker, "jira", nil)
	require.NoError(t, err)
	require.Len(t, plan.SecretFiles, 2)
	for _, file := range plan.SecretFiles {
		require.True(t, file.Present)
		require.Empty(t, file.Problem)
	}
}

func TestPlanConnectNeverCarriesSecretValue(t *testing.T) {
	_, cfgPath := connectFixture(t, "", map[string]string{"jira_api_token": plantedConnectSecret})
	cfg := mustLoad(t, cfgPath)
	reg, err := defaultRegistry()
	require.NoError(t, err)
	plan, err := PlanConnect(reg, cfg, registry.KindTracker, "jira", nil)
	require.NoError(t, err)
	require.NotContains(t, fmt.Sprintf("%+v %v", plan, plan), plantedConnectSecret)
}

func TestPlanConnectRefusesUnknownName(t *testing.T) {
	_, cfgPath := connectFixture(t, "", nil)
	cfg := mustLoad(t, cfgPath)
	reg, err := defaultRegistry()
	require.NoError(t, err)
	_, err = PlanConnect(reg, cfg, registry.KindVCS, "gitlab", nil)
	require.ErrorIs(t, err, registry.ErrUnknownName)
	require.Contains(t, err.Error(), "github")
}

func TestPlanConnectMattermostNeedsPublicAddressAnswer(t *testing.T) {
	_, cfgPath := connectFixture(t, "", nil)
	cfg := mustLoad(t, cfgPath)
	reg, err := defaultRegistry()
	require.NoError(t, err)

	_, err = PlanConnect(reg, cfg, registry.KindChat, "mattermost", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "public_address")

	plan, err := PlanConnect(reg, cfg, registry.KindChat, "mattermost", map[string]string{"public_address": "internal only"})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"chat.bridge": "mattermost"}, plan.Selection)
	require.Contains(t, strings.Join(plan.Instructions, "\n"), "internal")

	plan, err = PlanConnect(reg, cfg, registry.KindChat, "mattermost", map[string]string{"public_address": "chat.example.org"})
	require.NoError(t, err)
	require.Contains(t, strings.Join(plan.Instructions, "\n"), "https://chat.example.org")
}

func TestPlanConnectRefusesAnswersTheEntryDoesNotAsk(t *testing.T) {
	_, cfgPath := connectFixture(t, "", nil)
	cfg := mustLoad(t, cfgPath)
	reg, err := defaultRegistry()
	require.NoError(t, err)
	_, err = PlanConnect(reg, cfg, registry.KindVCS, "github", map[string]string{"public_address": "chat.example.org"})
	require.Error(t, err)
}

func TestParsePublicAddress(t *testing.T) {
	cases := []struct {
		in       string
		host     string
		internal bool
		wantErr  bool
	}{
		{"internal only", "", true, false},
		{"None", "", true, false},
		{"chat.example.org", "chat.example.org", false, false},
		{"Chat.Example.org", "chat.example.org", false, false},
		{"https://chat.example.org", "", false, true},
		{"chat.example.org:8065", "", false, true},
		{"chat.example.org/path", "", false, true},
		{"localhost", "", false, true},
		{"", "", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			host, internal, err := parsePublicAddress(tc.in)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.host, host)
			require.Equal(t, tc.internal, internal)
		})
	}
}

func TestConnectSelectionFieldKeys(t *testing.T) {
	require.Equal(t, "chat.bridge", registry.SelectionField(registry.KindChat))
	require.Equal(t, "tracker.provider", registry.SelectionField(registry.KindTracker))
	require.Equal(t, "vcs.provider", registry.SelectionField(registry.KindVCS))
}

func TestWriteSelectionRoundTripKeepsProjectsAndMode(t *testing.T) {
	extra := "[tracker]\nprovider = \"file\"\npath = \"/tmp/tickets\"\n\n[[projects]]\ntracker_project = \"PAY\"\nrepo = \"acme/pay\"\nbase_branch = \"main\"\n"
	dir, cfgPath := connectFixture(t, extra, nil)
	require.NoError(t, writeSelection(cfgPath, map[string]string{"tracker.provider": "jira"}))
	cfg := mustLoad(t, cfgPath)
	name, ok := cfg.Selection("tracker")
	require.True(t, ok)
	require.Equal(t, "jira", name)
	p, ok := cfg.ProjectFor("PAY")
	require.True(t, ok)
	require.Equal(t, "acme/pay", p.Repo)
	info, err := os.Stat(cfgPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	require.Equal(t, []string{"heimdall.toml", "secrets"}, names, "no temporary file may be left behind")
}

func TestWriteSelectionAddsMissingSection(t *testing.T) {
	_, cfgPath := connectFixture(t, "", nil)
	require.NoError(t, writeSelection(cfgPath, map[string]string{"chat.bridge": "teams"}))
	name, ok := mustLoad(t, cfgPath).Selection("chat")
	require.True(t, ok)
	require.Equal(t, "teams", name)
}

func TestWriteSelectionRefusesFileThatDoesNotLoad(t *testing.T) {
	_, cfgPath := connectFixture(t, "bogus_key = 1\n", nil)
	before, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	require.Error(t, writeSelection(cfgPath, map[string]string{"tracker.provider": "jira"}))
	after, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	require.Equal(t, before, after, "a file that does not load strictly must not be rewritten")
}

func TestRunConnectKindWritesSelectionAndInstructions(t *testing.T) {
	dir, cfgPath := connectFixture(t, "", nil)
	var out, errOut bytes.Buffer
	code := runConnect([]string{"--config", cfgPath, "vcs", "github"}, strings.NewReader(""), &out, &errOut, testConnectDeps())
	require.Equal(t, 0, code, errOut.String())
	text := out.String()
	require.Contains(t, text, filepath.Join(dir, "secrets", "github_app_private_key"))
	require.Contains(t, text, "needs_config")
	name, ok := mustLoad(t, cfgPath).Selection("vcs")
	require.True(t, ok)
	require.Equal(t, "github", name)
}

func TestRunConnectMattermostTakesPublicAddressFlag(t *testing.T) {
	_, cfgPath := connectFixture(t, "", nil)
	var out, errOut bytes.Buffer
	code := runConnect([]string{"--config", cfgPath, "--public-address", "chat.example.org", "chat", "mattermost"},
		strings.NewReader(""), &out, &errOut, testConnectDeps())
	require.Equal(t, 0, code, errOut.String())
	require.Contains(t, out.String(), "https://chat.example.org")
	name, ok := mustLoad(t, cfgPath).Selection("chat")
	require.True(t, ok)
	require.Equal(t, "mattermost", name)
}

func TestRunConnectArgumentErrorsAreUsageAndWriteNothing(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"no arguments", nil},
		{"extra argument", []string{"vcs", "github", "extra"}},
		{"unknown kind", []string{"nope", "x"}},
		{"secret as a flag", []string{"--token", plantedConnectSecret, "vcs", "github"}},
		{"secret as an argument", []string{"vcs", "github", plantedConnectSecret}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, cfgPath := connectFixture(t, "", nil)
			before, err := os.ReadFile(cfgPath)
			require.NoError(t, err)
			var out, errOut bytes.Buffer
			args := append([]string{"--config", cfgPath}, tc.args...)
			if tc.args == nil {
				args = nil
			}
			code := runConnect(args, strings.NewReader(""), &out, &errOut, testConnectDeps())
			require.Equal(t, 2, code, "stderr: %s", errOut.String())
			require.NotContains(t, out.String()+errOut.String(), plantedConnectSecret)
			after, err := os.ReadFile(cfgPath)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}

func TestRunConnectPickerChoosesByNumberThenAsksPublicAddress(t *testing.T) {
	_, cfgPath := connectFixture(t, "", nil)
	var out, errOut bytes.Buffer
	stdin := strings.NewReader("9\n1\nchat.example.org\n")
	code := runConnect([]string{"--config", cfgPath, "chat"}, stdin, &out, &errOut, testConnectDeps())
	require.Equal(t, 0, code, errOut.String())
	require.Contains(t, out.String(), "mattermost")
	require.Contains(t, out.String(), "teams")
	name, ok := mustLoad(t, cfgPath).Selection("chat")
	require.True(t, ok)
	require.Equal(t, "mattermost", name)
}

func TestRunConnectPickerChoosesByName(t *testing.T) {
	_, cfgPath := connectFixture(t, "", nil)
	var out, errOut bytes.Buffer
	code := runConnect([]string{"--config", cfgPath, "chat"}, strings.NewReader("teams\n"), &out, &errOut, testConnectDeps())
	require.Equal(t, 0, code, errOut.String())
	name, ok := mustLoad(t, cfgPath).Selection("chat")
	require.True(t, ok)
	require.Equal(t, "teams", name)
}

func TestRunConnectPickerNeverEchoesOrStoresTypedSecret(t *testing.T) {
	_, cfgPath := connectFixture(t, "", nil)
	before, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	var out, errOut bytes.Buffer
	// The picker reads choices only. A pasted secret is an invalid choice, it is never echoed, and input ends.
	code := runConnect([]string{"--config", cfgPath, "chat"}, strings.NewReader(plantedConnectSecret+"\n"), &out, &errOut, testConnectDeps())
	require.Equal(t, 1, code)
	require.NotContains(t, out.String()+errOut.String(), plantedConnectSecret)
	after, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	require.Equal(t, before, after)
}
