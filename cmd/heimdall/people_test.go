package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/policyyaml"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
)

// fakeDirectory is a Switch directory that answers with the users it holds, or with err.
type fakeDirectory struct {
	users []switchUser
	err   error
	calls int
}

func (f *fakeDirectory) ListUsers(context.Context) ([]switchUser, error) {
	f.calls++
	return f.users, f.err
}

func testPeopleDeps(dir switchDirectory) peopleDeps {
	return peopleDeps{directory: dir, deadline: 10 * time.Second}
}

// configuredPeopleDeps leaves the injected directory nil, so --config wiring builds one through newClient. The fake
// constructor records the config it received and returns a canned directory.
func configuredPeopleDeps(t *testing.T, dir switchDirectory, got *config.Config) peopleDeps {
	t.Helper()
	return peopleDeps{
		loadConfig: config.Load,
		getenv:     func(string) string { return "" },
		newClient: func(cfg config.Config) (switchDirectory, error) {
			*got = cfg
			return dir, nil
		},
		deadline: 10 * time.Second,
	}
}

// switchConfigFile writes a heimdall.toml with a complete [switch] section and returns its path.
func switchConfigFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "heimdall.toml")
	raw := "secrets_dir = \"" + filepath.Join(dir, "secrets") + "\"\n\n[switch]\n" +
		"base_url = \"https://switch.example.test\"\n" +
		"agent_id = \"agent-coord-1\"\n" +
		"agent_name = \"heimdall-coordinator\"\n" +
		"bridge_id = \"bridge-1\"\n" +
		"api_key_secret = \"switch_api_key\"\n" +
		"registration_token_secret = \"switch_registration_token\"\n"
	require.NoError(t, os.WriteFile(path, []byte(raw), 0o600))
	return path
}

// specPolicyBytes is the spec example that validates (TECH_SPEC 16), read from the policy package's fixtures.
func specPolicyBytes(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../internal/domain/policy/testdata/policy.golden.yaml")
	require.NoError(t, err)
	return raw
}

// policyFile writes raw to heimdall.yaml in a temp dir and returns the path.
func policyFile(t *testing.T, raw []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "heimdall.yaml")
	require.NoError(t, os.WriteFile(path, raw, 0o600))
	return path
}

// peopleRun runs the command and returns its exit code and output.
func peopleRun(t *testing.T, stdin string, deps peopleDeps, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := runConnectPeople(args, strings.NewReader(stdin), &out, &errb, deps)
	return code, out.String(), errb.String()
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	return raw
}

func TestConnectPeopleRefusesSenderIDOfAnotherPersonAndKeepsFile(t *testing.T) {
	raw := specPolicyBytes(t)
	path := policyFile(t, raw)
	code, _, errb := peopleRun(t, "", testPeopleDeps(&fakeDirectory{}),
		"--policy", path, "--name", "rev-backend", "--sender", "alice")
	require.Equal(t, connectExitFail, code)
	require.Contains(t, errb, "already the sender id of people.alice")
	require.Equal(t, raw, readFile(t, path), "a refused mapping must not change the file")
}

func TestConnectPeopleRefusesSecondSenderIDForSamePerson(t *testing.T) {
	raw := specPolicyBytes(t)
	path := policyFile(t, raw)
	code, _, errb := peopleRun(t, "", testPeopleDeps(&fakeDirectory{}),
		"--policy", path, "--name", "alice", "--sender", "alice-2")
	require.Equal(t, connectExitFail, code)
	require.Contains(t, errb, "edit heimdall.yaml to change it")
	require.Equal(t, raw, readFile(t, path))
}

func TestConnectPeopleRefusesUnknownRole(t *testing.T) {
	raw := specPolicyBytes(t)
	path := policyFile(t, raw)
	code, _, errb := peopleRun(t, "", testPeopleDeps(&fakeDirectory{}),
		"--policy", path, "--name", "dana", "--roles", "admin")
	require.Equal(t, connectExitFail, code)
	require.Contains(t, errb, "unknown role")
	require.Equal(t, raw, readFile(t, path))
}

func TestConnectPeopleRequiresGitHubForAuthor(t *testing.T) {
	raw := specPolicyBytes(t)
	path := policyFile(t, raw)
	code, _, errb := peopleRun(t, "", testPeopleDeps(&fakeDirectory{}),
		"--policy", path, "--name", "rev-backend", "--sender", "rev-backend-id", "--roles", "author")
	require.Equal(t, connectExitFail, code)
	require.Contains(t, errb, "people.rev-backend.github")
	require.Equal(t, raw, readFile(t, path))
}

func TestConnectPeopleRefusesApproverRoleOutsideApproverGroups(t *testing.T) {
	raw := specPolicyBytes(t)
	path := policyFile(t, raw)
	code, _, errb := peopleRun(t, "", testPeopleDeps(&fakeDirectory{}),
		"--policy", path, "--name", "dana", "--roles", "approver")
	require.Equal(t, connectExitFail, code)
	require.Contains(t, errb, "approvers group")
	require.Equal(t, raw, readFile(t, path))
}

func TestConnectPeopleRefusesGitHubLoginOfAnotherPerson(t *testing.T) {
	raw := specPolicyBytes(t)
	path := policyFile(t, raw)
	code, _, _ := peopleRun(t, "", testPeopleDeps(&fakeDirectory{}),
		"--policy", path, "--name", "rev-backend", "--sender", "rev-backend-id", "--github", "ALICE")
	require.Equal(t, connectExitFail, code)
	require.Equal(t, raw, readFile(t, path))
}

func TestConnectPeopleListUsesConfiguredSwitchClient(t *testing.T) {
	dir := &fakeDirectory{users: []switchUser{{Name: "Dana Smith", SenderID: "@dana", Type: "user"}}}
	var got config.Config
	path := switchConfigFile(t)
	deps := configuredPeopleDeps(t, dir, &got)

	code, out, errb := peopleRun(t, "", deps, "--list", "--config", path)
	require.Equal(t, 0, code, errb)
	require.Equal(t, 1, dir.calls)
	require.Equal(t, "https://switch.example.test", got.Switch.BaseURL)
	require.Equal(t, "agent-coord-1", got.Switch.AgentID)
	require.Equal(t, "heimdall-coordinator", got.Switch.AgentName)
	require.Equal(t, "bridge-1", got.Switch.BridgeID)
	require.Contains(t, out, "@dana")
}

func TestConnectPeopleListReportsDirectoryFailure(t *testing.T) {
	dir := &fakeDirectory{err: errors.New("switch is offline")}
	code, _, errb := peopleRun(t, "", testPeopleDeps(dir), "--list")
	require.Equal(t, connectExitFail, code)
	require.Contains(t, errb, "switch is offline")
}

func TestConnectPeopleListRefusesMappingFlags(t *testing.T) {
	dir := &fakeDirectory{}
	code, _, _ := peopleRun(t, "", testPeopleDeps(dir), "--list", "--name", "dana")
	require.Equal(t, connectExitUsage, code)
	require.Equal(t, 0, dir.calls, "a bad command line must not reach the directory")
}

func TestConnectPeopleRefusesPositionalArgumentsAndLooseFlags(t *testing.T) {
	raw := specPolicyBytes(t)
	path := policyFile(t, raw)
	code, _, _ := peopleRun(t, "", testPeopleDeps(&fakeDirectory{}), "--policy", path, "dana")
	require.Equal(t, connectExitUsage, code)

	code, _, errb := peopleRun(t, "", testPeopleDeps(&fakeDirectory{}), "--policy", path, "--sender", "x")
	require.Equal(t, connectExitUsage, code)
	require.Contains(t, errb, "--name")
	require.Equal(t, raw, readFile(t, path))
}

func TestConnectPeopleInteractiveMapsTwoPeople(t *testing.T) {
	path := policyFile(t, specPolicyBytes(t))
	// Per person: name, sender id, github, agent, roles. A blank name ends the list.
	stdin := "rev-a\nrev-a-id\n\n\n\nrev-b\nrev-b-id\nrev-b-gh\n\nauthor\n\n"
	code, out, errb := peopleRun(t, stdin, testPeopleDeps(&fakeDirectory{}), "--policy", path)
	require.Equal(t, 0, code, errb)
	require.Contains(t, out, "Person name")

	pol, err := policyyaml.Decode(readFile(t, path))
	require.NoError(t, err)
	require.Equal(t, "rev-a-id", pol.People["rev-a"].Chat)
	require.Equal(t, "rev-b-id", pol.People["rev-b"].Chat)
	require.Equal(t, "rev-b-gh", pol.People["rev-b"].GitHub)
}

func TestConnectPeopleInteractiveSelectsSenderByNumber(t *testing.T) {
	path := policyFile(t, specPolicyBytes(t))
	dir := &fakeDirectory{users: []switchUser{
		{Name: "Dana Smith", SenderID: "@dana", Type: "user"},
		{Name: "Eve", SenderID: "@eve", Type: "user"},
		{Name: "reviewer", SenderID: "reviewer", Type: "agent"},
	}}
	// Name, sender number, github, agent, roles, then a blank name to finish.
	stdin := "rev-a\n2\n\n\n\n\n"
	code, out, errb := peopleRun(t, stdin, testPeopleDeps(dir), "--policy", path)
	require.Equal(t, 0, code, errb)
	require.Equal(t, 1, dir.calls, "the directory is consulted once to show the picker")
	require.Contains(t, out, "Switch users")
	require.Contains(t, out, "Dana Smith")
	require.Contains(t, out, "@eve")
	require.NotContains(t, out, "reviewer", "agents are not offered as people")

	pol, err := policyyaml.Decode(readFile(t, path))
	require.NoError(t, err)
	require.Equal(t, "@eve", pol.People["rev-a"].Chat)
}

func TestConnectPeopleInteractiveOfflineStillTakesTypedSenderID(t *testing.T) {
	path := policyFile(t, specPolicyBytes(t))
	var got config.Config
	deps := peopleDeps{
		loadConfig: config.Load,
		getenv:     func(string) string { return "" },
		newClient: func(cfg config.Config) (switchDirectory, error) {
			got = cfg
			return nil, errors.New("no switch configured")
		},
		deadline: 10 * time.Second,
	}
	stdin := "rev-a\n@typed-id\n\n\n\n\n"
	code, out, errb := peopleRun(t, stdin, deps, "--policy", path)
	require.Equal(t, 0, code, errb)
	require.Contains(t, errb, "type sender ids by hand")
	require.NotContains(t, out, "Switch users")

	pol, err := policyyaml.Decode(readFile(t, path))
	require.NoError(t, err)
	require.Equal(t, "@typed-id", pol.People["rev-a"].Chat)
	require.Equal(t, config.Config{}, got, "an unusable client must not be built")
}

func TestConnectPeopleInteractiveDirectoryErrorFallsBackToTypedSenderID(t *testing.T) {
	path := policyFile(t, specPolicyBytes(t))
	dir := &fakeDirectory{err: errors.New("switch is offline")}
	stdin := "rev-a\n@typed-id\n\n\n\n\n"
	code, out, errb := peopleRun(t, stdin, testPeopleDeps(dir), "--policy", path)
	require.Equal(t, 0, code, errb)
	require.Contains(t, out, "Switch listing unavailable")
	require.Contains(t, out, "type each sender id by hand")

	pol, err := policyyaml.Decode(readFile(t, path))
	require.NoError(t, err)
	require.Equal(t, "@typed-id", pol.People["rev-a"].Chat)
}

// TestConnectPeopleInteractiveRefusedEntryIsNotSaved: a refused entry is reported and the owner is asked again. The
// file is written only at the end, so nothing changes here.
func TestConnectPeopleInteractiveRefusedEntryIsNotSaved(t *testing.T) {
	raw := specPolicyBytes(t)
	path := policyFile(t, raw)
	code, out, errb := peopleRun(t, "alice\nalice-2\n\n\n\n\n", testPeopleDeps(&fakeDirectory{}), "--policy", path)
	require.Equal(t, 0, code, errb)
	require.Contains(t, out, "not saved")
	require.Equal(t, raw, readFile(t, path))
}

func TestConnectPeopleNeverWritesAPolicyThatDoesNotLoad(t *testing.T) {
	raw := []byte("version: 2\n")
	path := policyFile(t, raw)
	code, _, errb := peopleRun(t, "", testPeopleDeps(&fakeDirectory{}), "--policy", path, "--name", "dana", "--sender", "dana-id")
	require.Equal(t, connectExitFail, code)
	require.Contains(t, errb, "heimdall.yaml")
	require.Equal(t, raw, readFile(t, path))
}

func TestRunConnectDispatchesPeopleSubcommand(t *testing.T) {
	t.Setenv("HEIMDALL_CONFIG", "")
	var out, errb bytes.Buffer
	code := runConnect([]string{"people", "--list"}, strings.NewReader(""), &out, &errb, realConnectDeps())
	require.Equal(t, connectExitFail, code)
	require.Contains(t, errb.String(), "config", "connect people must reach the people command and its config wiring")
	require.NotContains(t, errb.String(), "not built")
}

func TestSetPeopleSectionAddsMissingKey(t *testing.T) {
	out, err := setPeopleSection([]byte("version: 1\n"), map[string]policy.Person{"dana": {Chat: "dana"}})
	require.NoError(t, err)
	require.Contains(t, string(out), "people:")
	require.Contains(t, string(out), "chat: dana")
}

func TestSetPeopleSectionRefusesTopLevelThatIsNotAMapping(t *testing.T) {
	_, err := setPeopleSection([]byte("- a\n- b\n"), nil)
	require.Error(t, err)
}
