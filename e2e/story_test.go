package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/e2e"
)

const validStory = `
name: demo
repo: owner/repo
base_branch: main
ticket:
  title: Add refunds
  description: Merchants can refund a payment.
  ac:
    - a refund cannot exceed the payment
    - a repeated request returns the original refund
people:
  author:   { chat: dev-author,   github: aarush323, role: author }
  approver: { chat: dev-approver, github: rekdfr,    role: approver }
  bob:      { chat: rev-backend,  role: reviewer }
good_pr: demo/good
bad_pr: demo/bad
injection_pr: demo/injection
planted:
  - { id: P1, finder: bob, file: svc/refund.go, lines: [10, 20], tier: E1, repro: svc/refund_partial_test.go }
expected_blocking_set: [P1]
profile: go
`

func load(t *testing.T, body string) (*e2e.Story, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return e2e.LoadStory(path)
}

func TestLoadStoryReadsAValidStory(t *testing.T) {
	s, err := load(t, validStory)
	require.NoError(t, err)
	require.Equal(t, "demo", s.Name)
	require.Equal(t, "owner/repo", s.Repo)
	require.Equal(t, "aarush323", s.Person("author").GitHub)
	require.Equal(t, []string{"P1"}, s.ExpectedBlockingSet)
	require.Equal(t, [2]int{10, 20}, s.Planted[0].Lines)
	require.Equal(t, []string{"bob"}, s.Reviewers())
}

func TestLoadStoryRefusesAnInvalidStory(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(string) string
		message string
	}{
		{"no repo", func(s string) string { return strings.Replace(s, "repo: owner/repo\n", "", 1) }, "repo"},
		{"no base branch", func(s string) string { return strings.Replace(s, "base_branch: main\n", "", 1) }, "base_branch"},
		{"no acceptance criteria", func(s string) string {
			return strings.Replace(s, "  ac:\n    - a refund cannot exceed the payment\n    - a repeated request returns the original refund\n", "", 1)
		}, "ac"},
		{"no author", func(s string) string { return strings.Replace(s, "role: author", "role: reviewer", 1) }, "author"},
		{"no approver", func(s string) string { return strings.Replace(s, "role: approver", "role: reviewer", 1) }, "approver"},
		{"approver is the author", func(s string) string { return strings.Replace(s, "github: rekdfr", "github: aarush323", 1) }, "approver"},
		{"unknown role", func(s string) string { return strings.Replace(s, "role: reviewer", "role: wizard", 1) }, "role"},
		{"unknown tier", func(s string) string { return strings.Replace(s, "tier: E1", "tier: E9", 1) }, "tier"},
		{"blocking id is not planted", func(s string) string {
			return strings.Replace(s, "expected_blocking_set: [P1]", "expected_blocking_set: [P7]", 1)
		}, "P7"},
		{"finder is not a person", func(s string) string { return strings.Replace(s, "finder: bob", "finder: nobody", 1) }, "nobody"},
		{"duplicate planted id", func(s string) string {
			return strings.Replace(s, "expected_blocking_set", "  - { id: P1, finder: bob, file: a.go, lines: [1, 2], tier: E1, repro: a_test.go }\nexpected_blocking_set", 1)
		}, "P1"},
		{"lines run backwards", func(s string) string { return strings.Replace(s, "lines: [10, 20]", "lines: [20, 10]", 1) }, "lines"},
		{"planted issue without a file once the story is defined", func(s string) string { return strings.Replace(s, "file: svc/refund.go, ", "", 1) }, "file"},
		{"unknown field", func(s string) string { return s + "\nsurprise: true\n" }, "surprise"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := load(t, tc.mutate(validStory))
			require.ErrorContains(t, err, tc.message)
		})
	}
}

func TestLoadStoryRefusesBadReferences(t *testing.T) {
	tests := []struct {
		name, add, message string
	}{
		{"unknown domain approver", "domain_approver: nobody\n", "domain_approver"},
		{"challenge of an unplanted finding", "challenge: { finding: P9, evidence: x }\n", "challenge"},
		{"challenge without evidence", "challenge: { finding: P1, evidence: '' }\n", "challenge"},
		{"a stranger who is mapped", "stranger: dev-author\n", "stranger"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := load(t, validStory+tc.add)
			require.ErrorContains(t, err, tc.message)
		})
	}
	s, err := load(t, validStory+"domain_approver: bob\nstranger: user\nchallenge: { finding: P1, evidence: cited }\nanswer: yes\nfix_branch: demo/fix\n")
	require.NoError(t, err)
	require.Equal(t, "bob", s.DomainApprover)
	require.Equal(t, "cited", s.Challenge.Evidence)
}

func TestAPendingStoryMayLeavePlantedLocationsOpen(t *testing.T) {
	body := strings.Replace(validStory, "profile: go", "profile: go\npending: T31", 1)
	body = strings.Replace(body, "file: svc/refund.go, lines: [10, 20], ", "", 1)
	body = strings.Replace(body, "repro: svc/refund_partial_test.go", "repro: pending", 1)
	s, err := load(t, body)
	require.NoError(t, err)
	require.Equal(t, "T31", s.Pending)
}

func TestTheShippedStoriesLoad(t *testing.T) {
	for _, name := range []string{"fixture-pay", "jurist"} {
		t.Run(name, func(t *testing.T) {
			s, err := e2e.LoadStory(filepath.Join("stories", name+".yaml"))
			require.NoError(t, err)
			require.Equal(t, name, s.Name)
		})
	}
}

func TestFixturePayHasTheDemoShape(t *testing.T) {
	s, err := e2e.LoadStory(filepath.Join("stories", "fixture-pay.yaml"))
	require.NoError(t, err)
	require.Equal(t, "aarush323/heimdall-fixture-pay", s.Repo)
	require.Equal(t, "aarush323", s.Person("author").GitHub)
	require.Equal(t, "rekdfr", s.Person("approver").GitHub)
	require.Len(t, s.Ticket.AC, 4, "D57: four acceptance criteria")
	require.ElementsMatch(t, []string{"bob", "eli", "ana"}, s.Reviewers())
	require.Len(t, s.Planted, 3, "D57: three planted findings")
	require.ElementsMatch(t, []string{"partial-refunds", "refund-ref-renamed", "refund-read-authz"}, s.PlantedIDs())
}
