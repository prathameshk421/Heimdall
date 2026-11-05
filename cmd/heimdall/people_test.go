package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/policyyaml"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
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

func TestConnectPeopleFlagsAddSenderIDAndWriteFile(t *testing.T) {
	path := policyFile(t, specPolicyBytes(t))
	code, _, errb := peopleRun(t, "", testPeopleDeps(&fakeDirectory{}),
		"--policy", path, "--name", "rev-backend", "--sender", "rev-backend-id")
	require.Equal(t, 0, code, errb)

	pol, err := policyyaml.Decode(readFile(t, path))
	require.NoError(t, err, "the file written must load strictly and validate")
	require.Equal(t, "rev-backend-id", pol.People["rev-backend"].Chat)
}

// TestConnectPeopleKeepsTheRestOfTheFile: only the people table is rewritten. The other keys and the top comment stay.
func TestConnectPeopleKeepsTheRestOfTheFile(t *testing.T) {
	path := policyFile(t, specPolicyBytes(t))
	code, _, errb := peopleRun(t, "", testPeopleDeps(&fakeDirectory{}),
		"--policy", path, "--name", "rev-backend", "--sender", "rev-backend-id")
	require.Equal(t, 0, code, errb)

	out := string(readFile(t, path))
	require.Contains(t, out, "# The policy example from docs/TECH_SPEC.md section 16")
	require.Contains(t, out, "ci_required:")
	require.Contains(t, out, "budgets:")
	require.Contains(t, out, "rev-backend")
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

func TestConnectPeopleGitHubIsOptionalForReviewers(t *testing.T) {
	path := policyFile(t, specPolicyBytes(t))
	code, _, errb := peopleRun(t, "", testPeopleDeps(&fakeDirectory{}),
		"--policy", path, "--name", "rev-backend", "--sender", "rev-backend-id")
	require.Equal(t, 0, code, errb)

	pol, err := policyyaml.Decode(readFile(t, path))
	require.NoError(t, err)
	require.Empty(t, pol.People["rev-backend"].GitHub, "github is optional for a reviewer (D158)")
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

func TestConnectPeopleListPrintsSwitchUsers(t *testing.T) {
	dir := &fakeDirectory{users: []switchUser{{Name: "Dana Smith", Type: "user"}, {Name: "bob-reviewer", Type: "agent"}}}
	code, out, errb := peopleRun(t, "", testPeopleDeps(dir), "--list")
	require.Equal(t, 0, code, errb)
	require.Equal(t, 1, dir.calls)
	require.Contains(t, out, "Dana Smith")
	require.Contains(t, out, "agent")
}

func TestConnectPeopleListReportsDirectoryNotBuilt(t *testing.T) {
	code, _, errb := peopleRun(t, "", testPeopleDeps(notBuiltDirectory{}), "--list")
	require.Equal(t, connectExitFail, code)
	require.Contains(t, errb, "not built yet")
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

func TestConnectPeopleInteractiveNothingToChangeWritesNothing(t *testing.T) {
	raw := specPolicyBytes(t)
	path := policyFile(t, raw)
	code, out, _ := peopleRun(t, "\n", testPeopleDeps(&fakeDirectory{}), "--policy", path)
	require.Equal(t, 0, code)
	require.Contains(t, out, "no change")
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
	var out, errb bytes.Buffer
	code := runConnect([]string{"people", "--list"}, strings.NewReader(""), &out, &errb, realConnectDeps())
	require.Equal(t, connectExitFail, code)
	require.Contains(t, errb.String(), "not built yet", "connect people must reach the people command")
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
