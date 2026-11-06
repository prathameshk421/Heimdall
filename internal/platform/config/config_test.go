package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeTOML(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "heimdall.toml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

const goodProject = `
[[projects]]
tracker_project = "KAN"
repo = "aarush323/PBL_TOS_RISK_ANALYZER"
base_branch = "heimdall-demo"
`

func TestLoadAcceptsValidFileAndDefaultsListen(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(writeTOML(t, "secrets_dir = \""+dir+"\"\n"+goodProject))
	require.NoError(t, err)
	require.Equal(t, dir, cfg.SecretsDir)
	require.Equal(t, ":8080", cfg.Listen, "listen defaults to :8080 when the key is absent")
	require.Len(t, cfg.Projects, 1)
	p, ok := cfg.ProjectFor("KAN")
	require.True(t, ok)
	require.Equal(t, Project{TrackerProject: "KAN", Repo: "aarush323/PBL_TOS_RISK_ANALYZER", BaseBranch: "heimdall-demo"}, p)
	_, ok = cfg.ProjectFor("PAY")
	require.False(t, ok, "an unmapped tracker project is not found")
}

func TestLoadAcceptsFileTrackerPath(t *testing.T) {
	dir := t.TempDir()
	trackerDir := t.TempDir()
	cfg, err := Load(writeTOML(t, "secrets_dir = \""+dir+"\"\n[tracker]\nprovider = \"file\"\npath = \""+trackerDir+"\"\n"+goodProject))
	require.NoError(t, err)
	require.Equal(t, trackerDir, cfg.TrackerPath)
}

func TestLoadRequiresAbsoluteFileTrackerPath(t *testing.T) {
	for _, tc := range []struct{ path, want string }{{"", "tracker.path"}, {"tickets", "absolute path"}} {
		t.Run(tc.want, func(t *testing.T) {
			body := "[tracker]\nprovider = \"file\"\n"
			if tc.path != "" {
				body += "path = \"" + tc.path + "\"\n"
			}
			_, err := Load(writeTOML(t, body+goodProject))
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestLoadDefaultsSecretsDirWhenAbsent(t *testing.T) {
	cfg, err := Load(writeTOML(t, "listen = \":9000\"\n"+goodProject))
	require.NoError(t, err)
	require.Equal(t, "/etc/heimdall/secrets", cfg.SecretsDir, "secrets_dir defaults to /etc/heimdall/secrets (D89)")
	require.Equal(t, ":9000", cfg.Listen)
}

func TestLoadRefusals(t *testing.T) {
	dir := t.TempDir()
	head := "secrets_dir = \"" + dir + "\"\n"
	cases := []struct {
		name    string
		toml    string
		wantErr string
	}{
		{"unknown top-level key", head + "bogus = 1\n", "bogus"},
		{"unknown key inside a project", head + "[[projects]]\ntracker_project = \"KAN\"\nrepo = \"a/b\"\nbase_branch = \"main\"\nextra = true\n", "extra"},
		{"empty secrets_dir", "secrets_dir = \"\"\n" + goodProject, "secrets_dir"},
		{"empty listen", head + "listen = \"\"\n" + goodProject, "listen"},
		{"listen without a port", head + "listen = \"localhost\"\n" + goodProject, "listen"},
		{"project missing tracker_project", head + "[[projects]]\nrepo = \"a/b\"\nbase_branch = \"main\"\n", "tracker_project"},
		{"project missing repo", head + "[[projects]]\ntracker_project = \"KAN\"\nbase_branch = \"main\"\n", "repo"},
		{"project missing base_branch", head + "[[projects]]\ntracker_project = \"KAN\"\nrepo = \"a/b\"\n", "base_branch"},
		{"duplicate tracker_project", head + goodProject + goodProject, "already mapped"},
		{"repo without a slash", head + "[[projects]]\ntracker_project = \"KAN\"\nrepo = \"ownername\"\nbase_branch = \"main\"\n", "repo"},
		{"repo with three parts", head + "[[projects]]\ntracker_project = \"KAN\"\nrepo = \"a/b/c\"\nbase_branch = \"main\"\n", "repo"},
		{"repo with an empty owner", head + "[[projects]]\ntracker_project = \"KAN\"\nrepo = \"/name\"\nbase_branch = \"main\"\n", "repo"},
		{"repo with spaces", head + "[[projects]]\ntracker_project = \"KAN\"\nrepo = \"a/b c\"\nbase_branch = \"main\"\n", "repo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(writeTOML(t, tc.toml))
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestLoadReportsEveryProblemAtOnce(t *testing.T) {
	head := "secrets_dir = \"" + t.TempDir() + "\"\n"
	toml := head +
		"[[projects]]\ntracker_project = \"KAN\"\nbase_branch = \"main\"\n" + // missing repo
		"[[projects]]\ntracker_project = \"KAN\"\nrepo = \"a/b\"\nbase_branch = \"main\"\n" // duplicate
	_, err := Load(writeTOML(t, toml))
	require.ErrorContains(t, err, "repo")
	require.ErrorContains(t, err, "already mapped")
}

func TestLoadMissingFileIsAnError(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "absent.toml"))
	require.ErrorContains(t, err, "absent.toml")
}

func TestSecretReadsValueAndTrimsNewline(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "jira_token"), []byte("tok-live-123\n"), 0o400))
	cfg := Config{SecretsDir: dir}
	got, err := cfg.Secret("jira_token")
	require.NoError(t, err)
	require.Equal(t, "tok-live-123", got)
}

func TestSecretRefusals(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "outside"), []byte("OUTSIDEVALUE-7\n"), 0o400))
	require.NoError(t, os.Symlink(filepath.Join(outside, "outside"), filepath.Join(dir, "linked")))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "empty"), []byte("\n"), 0o400))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "multi"), []byte("SECRETVALUE-42\nsecond line\n"), 0o400))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "adir"), 0o700))
	cfg := Config{SecretsDir: dir}

	cases := []struct {
		name    string
		secret  string
		wantErr string
	}{
		{"parent traversal", "../outside", "../outside"},
		{"bare dotdot", "..", ".."},
		{"nested path", "sub/token", "sub/token"},
		{"backslash path", `sub\token`, "path separator"},
		{"dot", ".", "."},
		{"empty name", "", "name"},
		{"missing file", "absent", "absent"},
		{"empty file", "empty", "empty"},
		{"symlink escaping the directory", "linked", "linked"},
		{"directory instead of file", "adir", "adir"},
		{"multi-line value is refused", "multi", "multi"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := cfg.Secret(tc.secret)
			require.Error(t, err)
			require.Empty(t, got)
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestSecretValuesNeverAppearInErrors(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "outside"), []byte("OUTSIDEVALUE-7\n"), 0o400))
	require.NoError(t, os.Symlink(filepath.Join(outside, "outside"), filepath.Join(dir, "linked")))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "multi"), []byte("SECRETVALUE-42\nsecond line\n"), 0o400))
	cfg := Config{SecretsDir: dir}

	for _, name := range []string{"linked", "multi"} {
		_, err := cfg.Secret(name)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "OUTSIDEVALUE-7", "error for %s leaked a value", name)
		require.NotContains(t, err.Error(), "SECRETVALUE-42", "error for %s leaked a value", name)
	}
}

func TestOnceSetReportsFirstTimeOnlyPerKey(t *testing.T) {
	var s OnceSet
	require.True(t, s.First("PAY"), "the first time a key is offered")
	require.False(t, s.First("PAY"), "the second time it is not")
	require.True(t, s.First("KAN"), "keys are independent")
}

func TestOnceSetIsSafeForConcurrentUse(t *testing.T) {
	var s OnceSet
	var wg sync.WaitGroup
	var mu sync.Mutex
	firsts := 0
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.First("NOPE") {
				mu.Lock()
				firsts++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	require.Equal(t, 1, firsts)
}

func TestFilePath(t *testing.T) {
	env := func(v string) func(string) string {
		return func(k string) string {
			if k == "HEIMDALL_CONFIG" {
				return v
			}
			return ""
		}
	}
	cases := []struct {
		name    string
		args    []string
		envVal  string
		want    string
		wantErr string
	}{
		{"--config with a separate value", []string{"--config", "/a/heimdall.toml"}, "/env/h.toml", "/a/heimdall.toml", ""},
		{"--config=value form", []string{"--config=/b.toml"}, "", "/b.toml", ""},
		{"flag wins over the environment", []string{"--config", "/a.toml"}, "/env.toml", "/a.toml", ""},
		{"environment when no flag", nil, "/env/h.toml", "/env/h.toml", ""},
		{"neither flag nor environment is an error", nil, "", "", "HEIMDALL_CONFIG"},
		{"unknown flag is an error", []string{"--bogus"}, "", "", "bogus"},
		{"--config without a value is an error", []string{"--config"}, "", "", "config"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := FilePath(tc.args, env(tc.envVal))
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestLoadReadsProviderSelections(t *testing.T) {
	cfg, err := Load(writeTOML(t, "secrets_dir = \""+t.TempDir()+"\"\n"+
		"[vcs]\nprovider = \"github\"\n"+
		"[tracker]\nprovider = \"jira\"\n"+
		"[chat]\nbridge = \"mattermost\"\n"+
		"[agent_runtime]\nprovider = \"switchroom\"\nmachine = \"heimdall-vm\"\nmcp_endpoint = \"http://127.0.0.1:8080/mcp\"\n"+
		"[memory]\nprovider = \"graphiti\"\n"))
	require.NoError(t, err)
	want := map[string]string{"vcs": "github", "tracker": "jira", "chat": "mattermost", "agent_runtime": "switchroom", "memory": "graphiti"}
	for kind, name := range want {
		got, ok := cfg.Selection(kind)
		require.True(t, ok, kind)
		require.Equal(t, name, got, kind)
	}
}

func TestSelectionAbsentIsUnset(t *testing.T) {
	cfg, err := Load(writeTOML(t, "secrets_dir = \""+t.TempDir()+"\"\n[vcs]\n"))
	require.NoError(t, err)
	for _, kind := range []string{"vcs", "tracker", "chat", "agent_runtime", "memory", "unknown"} {
		got, ok := cfg.Selection(kind)
		require.False(t, ok, "%s must be unset", kind)
		require.Empty(t, got)
	}
}

func TestLoadRefusesEmptySelections(t *testing.T) {
	cases := []struct {
		name    string
		section string
	}{
		{"vcs.provider", "[vcs]\nprovider = \"\"\n"},
		{"tracker.provider", "[tracker]\nprovider = \"\"\n"},
		{"chat.bridge", "[chat]\nbridge = \"\"\n"},
		{"agent_runtime.provider", "[agent_runtime]\nprovider = \"\"\n"},
		{"memory.provider", "[memory]\nprovider = \"\"\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(writeTOML(t, "secrets_dir = \""+t.TempDir()+"\"\n"+tc.section))
			require.ErrorContains(t, err, tc.name+" must not be empty")
		})
	}
}

func TestLoadRefusesUnknownSelectionKeys(t *testing.T) {
	cases := []struct {
		name    string
		section string
	}{
		{"typo in key name", "[vcs]\nprovidr = \"github\"\n"},
		{"chat takes bridge, not provider", "[chat]\nprovider = \"mattermost\"\n"},
		{"unknown section", "[forge]\nprovider = \"github\"\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(writeTOML(t, "secrets_dir = \""+t.TempDir()+"\"\n"+tc.section))
			require.ErrorContains(t, err, "unknown key")
		})
	}
}
