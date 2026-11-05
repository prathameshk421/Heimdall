package registry

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/stretchr/testify/require"
)

// plantedSecret is written to a secret file in the test directory. No Detail may ever contain it.
const plantedSecret = "PLANTED-SECRET-VALUE-7f3a9c"

// stubEntry is a test-only adapter. It proves that an adapter in its own package is selectable by name
// with no change anywhere else: nothing in this package knows its name.
func stubEntry(name string, kind Kind, required ...string) Entry {
	return Entry{
		Name:           name,
		Kind:           kind,
		RequiredConfig: required,
		Factory: func(config.Config) (any, error) {
			return name + "-adapter", nil
		},
		Check: func(_ context.Context, _ config.Config) CheckResult {
			return CheckResult{Status: StatusConnected, Detail: name + " answered"}
		},
	}
}

// loadConfig writes heimdall.toml content to a temp dir and loads it through config.Load, the same path the server uses.
func loadConfig(t *testing.T, extra string) config.Config {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "heimdall.toml")
	require.NoError(t, os.WriteFile(path, []byte("secrets_dir = \""+dir+"\"\n"+extra), 0o600))
	cfg, err := config.Load(path)
	require.NoError(t, err)
	return cfg
}

func TestKindsAreFixedAndValid(t *testing.T) {
	require.Equal(t, []Kind{KindVCS, KindTracker, KindChat, KindAgentRuntime, KindMemory}, Kinds())
	for _, k := range Kinds() {
		require.True(t, k.Valid(), "kind %q", k)
	}
	require.False(t, Kind("nope").Valid())
	require.False(t, Kind("").Valid())
}

func TestRegisterRefusesInvalidEntries(t *testing.T) {
	noop := func(config.Config) (any, error) { return nil, nil }
	noCheck := func(context.Context, config.Config) CheckResult { return CheckResult{} }
	cases := []struct {
		name  string
		entry Entry
	}{
		{"empty name", Entry{Name: "", Kind: KindVCS, Factory: noop, Check: noCheck}},
		{"name with uppercase", Entry{Name: "GitHub", Kind: KindVCS, Factory: noop, Check: noCheck}},
		{"name starting with uppercase", Entry{Name: "Github", Kind: KindVCS, Factory: noop, Check: noCheck}},
		{"name with a space", Entry{Name: "git hub", Kind: KindVCS, Factory: noop, Check: noCheck}},
		{"name with a slash", Entry{Name: "git/hub", Kind: KindVCS, Factory: noop, Check: noCheck}},
		{"unknown kind", Entry{Name: "x", Kind: "nope", Factory: noop, Check: noCheck}},
		{"missing kind", Entry{Name: "x", Factory: noop, Check: noCheck}},
		{"nil factory", Entry{Name: "x", Kind: KindVCS, Check: noCheck}},
		{"nil check", Entry{Name: "x", Kind: KindVCS, Factory: noop}},
		{"empty required config", Entry{Name: "x", Kind: KindVCS, RequiredConfig: []string{""}, Factory: noop, Check: noCheck}},
		{"required config with dot-dot", Entry{Name: "x", Kind: KindVCS, RequiredConfig: []string{"a..b"}, Factory: noop, Check: noCheck}},
		{"required config with a slash", Entry{Name: "x", Kind: KindVCS, RequiredConfig: []string{"sub/token"}, Factory: noop, Check: noCheck}},
		{"duplicate required config", Entry{Name: "x", Kind: KindVCS, RequiredConfig: []string{"tok", "tok"}, Factory: noop, Check: noCheck}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var r Registry
			err := r.Register(tc.entry)
			require.ErrorIs(t, err, ErrInvalidEntry)
			require.Empty(t, r.Names(KindVCS), "a refused entry is not registered")
		})
	}
}

func TestRegisterRefusesDuplicateKindAndName(t *testing.T) {
	var r Registry
	require.NoError(t, r.Register(stubEntry("github", KindVCS)))
	err := r.Register(stubEntry("github", KindVCS))
	require.ErrorIs(t, err, ErrDuplicate)

	require.NoError(t, r.Register(stubEntry("github", KindTracker)), "the same name under another kind is a different integration")
	require.Equal(t, []string{"github"}, r.Names(KindVCS))
	require.Equal(t, []string{"github"}, r.Names(KindTracker))
}

func TestSelectReturnsTheRegisteredEntry(t *testing.T) {
	var r Registry
	require.NoError(t, r.Register(stubEntry("github", KindVCS, "github_app_key")))
	e, err := r.Select(KindVCS, "github")
	require.NoError(t, err)
	require.Equal(t, "github", e.Name)
	require.Equal(t, KindVCS, e.Kind)
	require.Equal(t, []string{"github_app_key"}, e.RequiredConfig)
	got, err := e.Factory(config.Config{})
	require.NoError(t, err)
	require.Equal(t, "github-adapter", got)
}

func TestSelectUnknownNameListsValidNamesForTheKind(t *testing.T) {
	var r Registry
	require.NoError(t, r.Register(stubEntry("github", KindVCS)))
	require.NoError(t, r.Register(stubEntry("gitlab", KindVCS)))
	require.NoError(t, r.Register(stubEntry("jira", KindTracker)))

	cases := []struct {
		name string
		kind Kind
		in   string
	}{
		{"typo", KindVCS, "gitub"},
		{"empty name", KindVCS, ""},
		{"wrong case", KindVCS, "GitHub"},
		{"a name registered under another kind", KindVCS, "jira"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := r.Select(tc.kind, tc.in)
			require.ErrorIs(t, err, ErrUnknownName)
			var se *SelectError
			require.ErrorAs(t, err, &se)
			require.Equal(t, []string{"github", "gitlab"}, se.Valid, "valid names are for the kind only, sorted")
			require.ErrorContains(t, err, "github")
			require.ErrorContains(t, err, "gitlab")
		})
	}
}

func TestSelectUnknownKindIsATypedError(t *testing.T) {
	var r Registry
	_, err := r.Select(Kind("nope"), "github")
	require.ErrorIs(t, err, ErrUnknownKind)
	require.ErrorContains(t, err, "nope")
}

func TestSelectOnEmptyRegistryNamesNoOptions(t *testing.T) {
	var r Registry
	_, err := r.Select(KindChat, "mattermost")
	require.ErrorIs(t, err, ErrUnknownName)
	var se *SelectError
	require.ErrorAs(t, err, &se)
	require.Empty(t, se.Valid)
}

func TestDescribeIsSortedDeterministically(t *testing.T) {
	var r Registry
	// Registered in an order that is neither sorted by kind nor by name.
	require.NoError(t, r.Register(stubEntry("slack", KindChat, "slack_token", "slack_signing")))
	require.NoError(t, r.Register(stubEntry("jira", KindTracker, "jira_token")))
	require.NoError(t, r.Register(stubEntry("github", KindVCS, "github_app_key")))
	require.NoError(t, r.Register(stubEntry("mattermost", KindChat, "mm_token")))
	require.NoError(t, r.Register(stubEntry("gitlab", KindVCS)))

	// Kinds in the declared order (vcs, tracker, chat, ...), names sorted within each kind.
	want := []Description{
		{Name: "github", Kind: KindVCS, RequiredConfig: []string{"github_app_key"}},
		{Name: "gitlab", Kind: KindVCS, RequiredConfig: nil},
		{Name: "jira", Kind: KindTracker, RequiredConfig: []string{"jira_token"}},
		{Name: "mattermost", Kind: KindChat, RequiredConfig: []string{"mm_token"}},
		{Name: "slack", Kind: KindChat, RequiredConfig: []string{"slack_signing", "slack_token"}},
	}
	require.Equal(t, want, r.Describe())
	require.Equal(t, want, r.Describe(), "the same registry describes the same way twice")
}

func TestDefaultRegistersTheGivenList(t *testing.T) {
	r, err := Default([]Entry{stubEntry("github", KindVCS), stubEntry("jira", KindTracker)})
	require.NoError(t, err)
	_, err = r.Select(KindTracker, "jira")
	require.NoError(t, err)

	_, err = Default([]Entry{stubEntry("github", KindVCS), stubEntry("github", KindVCS)})
	require.ErrorIs(t, err, ErrDuplicate, "Default fails on a duplicate in the list")

	_, err = Default([]Entry{{Name: "x", Kind: "nope"}})
	require.ErrorIs(t, err, ErrInvalidEntry, "Default fails on an invalid entry in the list")
}

// requireNoSecret fails if the planted secret value appears in any part of the result.
func requireNoSecret(t *testing.T, res CheckResult, secret string) {
	t.Helper()
	require.NotContains(t, string(res.Status), secret)
	require.NotContains(t, res.Detail, secret, "Check detail must never carry a secret value")
}

func TestCheckDetailNeverCarriesASecretValue(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stub_token"), []byte(plantedSecret+"\n"), 0o400))
	entry := stubEntry("stubvcs", KindVCS, "stub_token")
	// The stub's check reads the secret and reports only what it found, the way a real adapter must.
	entry.Check = func(_ context.Context, c config.Config) CheckResult {
		if _, err := c.Secret("stub_token"); err != nil {
			return CheckResult{Status: StatusNeedsConfig, Detail: "create the secret file stub_token"}
		}
		return CheckResult{Status: StatusConnected, Detail: "stub answered with the secret file present"}
	}
	res := entry.Check(context.Background(), loadedWithSecretsDir(t, dir))
	require.Equal(t, StatusConnected, res.Status)
	requireNoSecret(t, res, plantedSecret)
}

// loadedWithSecretsDir loads a config whose secrets_dir is dir.
func loadedWithSecretsDir(t *testing.T, dir string) config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "heimdall.toml")
	require.NoError(t, os.WriteFile(path, []byte("secrets_dir = \""+dir+"\"\n"), 0o600))
	cfg, err := config.Load(path)
	require.NoError(t, err)
	return cfg
}

func TestValidateSelections(t *testing.T) {
	var r Registry
	require.NoError(t, r.Register(stubEntry("github", KindVCS)))
	require.NoError(t, r.Register(stubEntry("jira", KindTracker)))
	require.NoError(t, r.Register(stubEntry("mattermost", KindChat)))

	cases := []struct {
		name    string
		toml    string
		wantErr []string // every string must appear in the error; none means no error
	}{
		{"no selections is valid", "", nil},
		{"valid selections pass", "[vcs]\nprovider = \"github\"\n[tracker]\nprovider = \"jira\"\n[chat]\nbridge = \"mattermost\"\n", nil},
		{"a typo names the key, the value and the valid options", "[vcs]\nprovider = \"gitub\"\n", []string{"vcs.provider", "gitub", "github"}},
		{"a bridge with no registered chat adapter is refused", "[chat]\nbridge = \"teams\"\n", []string{"chat.bridge", "teams"}},
		{"every problem is reported at once", "[vcs]\nprovider = \"gitub\"\n[tracker]\nprovider = \"jra\"\n", []string{"vcs.provider", "gitub", "tracker.provider", "jra", "jira"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := loadConfig(t, tc.toml)
			err := ValidateSelections(cfg, &r)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			for _, s := range tc.wantErr {
				require.ErrorContains(t, err, s)
			}
		})
	}
}

func TestStubAdapterIsSelectableByNameWithNoChangeOutsideItsPackage(t *testing.T) {
	var r Registry
	require.NoError(t, r.Register(stubEntry("stubvcs", KindVCS, "stub_token")))

	cfg := loadConfig(t, "[vcs]\nprovider = \"stubvcs\"\n")
	require.NoError(t, ValidateSelections(cfg, &r))

	name, ok := cfg.Selection(string(KindVCS))
	require.True(t, ok)
	e, err := r.Select(KindVCS, name)
	require.NoError(t, err)
	got, err := e.Factory(cfg)
	require.NoError(t, err)
	require.Equal(t, "stubvcs-adapter", got)
	res := e.Check(context.Background(), cfg)
	require.Equal(t, StatusConnected, res.Status)
	require.Contains(t, res.Detail, "stubvcs")
}
