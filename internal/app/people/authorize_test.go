package people

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// headSHA is the current head of the change in every test. staleSHA is an older commit of the same PR.
const (
	headSHA  id.SHA = "3f2a1c9d8e7b6a5f4e3d2c1b0a9f8e7d6c5b4a39"
	staleSHA id.SHA = "0123456789abcdef0123456789abcdef01234567"
)

// openChange is a PR by dana whose changed paths belong to the auth approver group.
func openChange() Change {
	return Change{HeadSHA: headSHA, AuthorLogin: "dana", DomainGroups: []string{"auth"}}
}

// attempt is a decision that names the current head. Chat commands carry no SHA, so the SHA-bound tests
// build their own attempts.
func attempt(ch Channel, sender string, action Action) Attempt {
	return Attempt{Channel: ch, Sender: sender, Action: action, SeenSHA: headSHA}
}

// allActions is every action the table must decide: the fifteen decision kinds of the grammar (TECH_SPEC
// 11.5) and the GitHub review approval.
func allActions() []Action {
	return []Action{
		change.DecisionReview, change.DecisionUphold, change.DecisionDismiss, change.DecisionSpecChange,
		change.DecisionOverride, change.DecisionWaiveReviewer, change.DecisionReject, change.DecisionApproveTests,
		change.DecisionIncident, change.DecisionStart, change.DecisionChallenge, change.DecisionRemember,
		change.DecisionForget, change.DecisionResume, change.DecisionAnswer, ActionApprove,
	}
}

// row is one line of TECH_SPEC 11.5 as a test: ok may take the action, no is refused with refused.
type row struct {
	action  Action
	channel Channel
	ok      string
	no      string
	refused error
	change  Change
}

func authorizeRows() []row {
	open := openChange()
	return []row{
		{change.DecisionReview, ChannelChat, "dana", "alice", ErrWrongPerson, open},
		{change.DecisionUphold, ChannelChat, "alice", "dana", ErrWrongPerson, open},
		{change.DecisionDismiss, ChannelChat, "alice", "dana", ErrWrongPerson, open},
		{change.DecisionSpecChange, ChannelChat, "alice", "dana", ErrWrongPerson, open},
		{change.DecisionOverride, ChannelChat, "alice", "dana", ErrWrongPerson, open},
		{change.DecisionWaiveReviewer, ChannelChat, "alice", "dana", ErrWrongPerson, open},
		{change.DecisionReject, ChannelChat, "alice", "dana", ErrWrongPerson, open},
		{change.DecisionApproveTests, ChannelChat, "ana", "alice", ErrWrongPerson, open},
		{change.DecisionIncident, ChannelChat, "dana", "nobody", ErrUnknownSender, open},
		{change.DecisionStart, ChannelChat, "dana", "nobody", ErrUnknownSender, open},
		{change.DecisionChallenge, ChannelChat, "dana", "alice", ErrWrongPerson, open},
		{change.DecisionRemember, ChannelChat, "dana", "nobody", ErrUnknownSender, open},
		{change.DecisionForget, ChannelChat, "alice", "dana", ErrWrongPerson, open},
		{change.DecisionResume, ChannelChat, "alice", "dana", ErrWrongPerson, open},
		{change.DecisionAnswer, ChannelChat, "dana", "nobody", ErrUnknownSender, open},
		{ActionApprove, ChannelGitHub, "alice", "eli", ErrWrongPerson, open},
	}
}

func TestTableCoversEveryActionAndOnlyDecisionKinds(t *testing.T) {
	table := rules()
	require.ElementsMatch(t, allActions(), keysOf(table), "the rule table decides exactly the actions")
	for _, a := range allActions() {
		if a == ActionApprove {
			continue
		}
		require.True(t, change.DecisionKind(a).Valid(), "%s is a grammar decision kind", a)
	}
	require.ElementsMatch(t, allActions(), rowActions(authorizeRows()), "each action has a row")
}

func keysOf(m map[Action]rule) []Action {
	out := make([]Action, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func rowActions(rows []row) []Action {
	out := make([]Action, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.action)
	}
	return out
}

// TestAuthorizeRowsMatchTheSpecTable has one subtest per action. The allowed sender passes, and the refused
// sender fails with the reason the spec gives for that row.
func TestAuthorizeRowsMatchTheSpecTable(t *testing.T) {
	d := newDir(t)
	for _, r := range authorizeRows() {
		t.Run(string(r.action), func(t *testing.T) {
			grant, err := d.Authorize(attempt(r.channel, r.ok, r.action), r.change)
			require.NoError(t, err)
			require.Equal(t, r.action, grant.Action)

			_, err = d.Authorize(attempt(r.channel, r.no, r.action), r.change)
			require.ErrorIs(t, err, r.refused)
		})
	}
}

func TestAuthorizeGrantNamesThePerson(t *testing.T) {
	d := newDir(t)
	grant, err := d.Authorize(attempt(ChannelChat, "ana", change.DecisionUphold), openChange())
	require.NoError(t, err)
	require.Equal(t, "ana", grant.Person)
}

func TestAuthorizeRefusesUnknownSenderForEveryAction(t *testing.T) {
	d := newDir(t)
	for _, action := range allActions() {
		ch := ChannelChat
		sender := "nobody"
		if action == ActionApprove {
			ch = ChannelGitHub
			sender = "ghost"
		}
		_, err := d.Authorize(attempt(ch, sender, action), openChange())
		require.ErrorIs(t, err, ErrUnknownSender, "action %s", action)
	}
}

// TestAuthorizeRefusesAgentSenderForEveryAction is invariant 1: an agent never decides, even for an action any
// human may take.
func TestAuthorizeRefusesAgentSenderForEveryAction(t *testing.T) {
	d := newDir(t)
	for _, action := range allActions() {
		if action == ActionApprove {
			continue
		}
		_, err := d.Authorize(attempt(ChannelChat, "bob-reviewer", action), openChange())
		require.ErrorIs(t, err, ErrAgentSender, "action %s", action)
	}
}

func TestAuthorizeRefusesUnknownAction(t *testing.T) {
	d := newDir(t)
	_, err := d.Authorize(attempt(ChannelChat, "alice", Action("make-coffee")), openChange())
	require.ErrorIs(t, err, ErrUnknownAction)
}

func TestAuthorizeRefusesAuthorWhoApprovesOwnChange(t *testing.T) {
	d := newDir(t)
	open := openChange()
	open.AuthorLogin = "ANA"
	_, err := d.Authorize(attempt(ChannelGitHub, "ana", ActionApprove), open)
	require.ErrorIs(t, err, ErrSelfApproval, "ana is an approver, and the author match ignores case")
}

func TestAuthorizeRefusesAuthorWhoApprovesOwnTests(t *testing.T) {
	d := newDir(t)
	open := openChange()
	open.AuthorLogin = "ana"
	_, err := d.Authorize(attempt(ChannelChat, "ana", change.DecisionApproveTests), open)
	require.ErrorIs(t, err, ErrSelfApproval)
}

func TestAuthorizeRefusesApprovalWithoutSHA(t *testing.T) {
	d := newDir(t)
	a := attempt(ChannelGitHub, "alice", ActionApprove)
	a.SeenSHA = ""
	_, err := d.Authorize(a, openChange())
	require.ErrorIs(t, err, ErrUnboundDecision)
}

func TestAuthorizeRefusesApprovalOfStaleSHA(t *testing.T) {
	d := newDir(t)
	a := attempt(ChannelGitHub, "alice", ActionApprove)
	a.SeenSHA = staleSHA
	_, err := d.Authorize(a, openChange())
	require.ErrorIs(t, err, ErrStaleSHA, "an approval counts only at the current head (invariant 2)")
}

func TestAuthorizeRefusesChatDecisionThatNamesAStaleSHA(t *testing.T) {
	d := newDir(t)
	a := attempt(ChannelChat, "alice", change.DecisionUphold)
	a.SeenSHA = staleSHA
	_, err := d.Authorize(a, openChange())
	require.ErrorIs(t, err, ErrStaleSHA)
}

func TestAuthorizeAcceptsChatDecisionWithoutSHA(t *testing.T) {
	d := newDir(t)
	a := attempt(ChannelChat, "alice", change.DecisionUphold)
	a.SeenSHA = ""
	_, err := d.Authorize(a, openChange())
	require.NoError(t, err, "the chat grammar names no SHA, so a chat decision is not SHA-bound")
}

func TestAuthorizeRefusesHeadThatIsNotAGitHash(t *testing.T) {
	d := newDir(t)
	ch := openChange()
	ch.HeadSHA = "not-a-sha"
	_, err := d.Authorize(attempt(ChannelChat, "alice", change.DecisionUphold), ch)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrWrongPerson, "a bad head is an input error, not a refusal of the sender")
}

// TestDismissAllowsTheFilingReviewerOnly: a dismiss is the filing reviewer's, or an approver's (TECH_SPEC 11.5).
func TestDismissAllowsTheFilingReviewerOnly(t *testing.T) {
	d := newDir(t)
	ch := openChange()
	ch.FilingPerson = "eli"
	_, err := d.Authorize(attempt(ChannelChat, "eli", change.DecisionDismiss), ch)
	require.NoError(t, err, "eli filed the finding")

	_, err = d.Authorize(attempt(ChannelChat, "dana", change.DecisionDismiss), ch)
	require.ErrorIs(t, err, ErrWrongPerson)
}

// TestApproveTestsNeedsAnApproverOfTheChangedDomain: a domain approver of another domain is refused.
func TestApproveTestsNeedsAnApproverOfTheChangedDomain(t *testing.T) {
	d := newDir(t)
	ch := openChange()
	ch.DomainGroups = []string{"money_movement"}

	_, err := d.Authorize(attempt(ChannelChat, "bob", change.DecisionApproveTests), ch)
	require.NoError(t, err, "bob approves money_movement")

	_, err = d.Authorize(attempt(ChannelChat, "ana", change.DecisionApproveTests), ch)
	require.ErrorIs(t, err, ErrWrongPerson, "ana approves auth, not money_movement")
}

func TestApproveTestsWithNoDomainGroupLetsNobodyApprove(t *testing.T) {
	d := newDir(t)
	ch := openChange()
	ch.DomainGroups = nil
	_, err := d.Authorize(attempt(ChannelChat, "ana", change.DecisionApproveTests), ch)
	require.ErrorIs(t, err, ErrWrongPerson)
}

// TestReviewNeedsTheAuthorsGitHubLogin: the author is matched by GitHub login, so a person with no login cannot
// be the author.
func TestReviewNeedsTheAuthorsGitHubLogin(t *testing.T) {
	p := specPolicy(t)
	p.People["rev-backend"] = policy.Person{Chat: "rev-backend-id"}
	d, err := New(p)
	require.NoError(t, err)

	ch := openChange()
	ch.AuthorLogin = "rev-backend"
	_, err = d.Authorize(attempt(ChannelChat, "rev-backend-id", change.DecisionReview), ch)
	require.ErrorIs(t, err, ErrWrongPerson)
}

// TestPersonWithoutGitHubStillDecidesAsMember: github is optional for a reviewer (D158), so a member with no login
// can still record a start or a remark.
func TestPersonWithoutGitHubStillDecidesAsMember(t *testing.T) {
	p := specPolicy(t)
	p.People["rev-backend"] = policy.Person{Chat: "rev-backend-id"}
	d, err := New(p)
	require.NoError(t, err)

	_, err = d.Authorize(attempt(ChannelChat, "rev-backend-id", change.DecisionIncident), openChange())
	require.NoError(t, err)
}
