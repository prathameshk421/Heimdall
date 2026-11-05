// Package gate is the merge rule: a pure function from what is known about a change to the decision and the
// plain-language reason for each clause that is not met (TECH_SPEC 12.4, invariants 2, 3 and 5 in 3.4).
package gate

import (
	"fmt"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/slop"
)

// CheckAcceptance is the check name Heimdall's own acceptance run reports under (TECH_SPEC 12.4, D150).
const CheckAcceptance = "heimdall/acceptance"

// ClauseID names one of the ten clauses of the merge rule. The id is stable, so a check summary, a log line and a
// test can refer to the same clause.
type ClauseID string

// The ten clauses of TECH_SPEC 12.4, in the order the rule states them.
const (
	ClauseTicketLinked   ClauseID = "ticket_linked"
	ClauseSlopPassed     ClauseID = "slop_passed"
	ClauseTestset        ClauseID = "testset"
	ClauseTestTreePinned ClauseID = "test_tree_pinned"
	ClauseTestDiffScoped ClauseID = "test_diff_scoped"
	ClauseCI             ClauseID = "ci"
	ClauseReviews        ClauseID = "reviews"
	ClauseBlocking       ClauseID = "blocking"
	ClauseApproval       ClauseID = "approval"
	ClausePolicy         ClauseID = "policy"
)

// Testset is what is known about the pinned test set (TECH_SPEC 12.1, 12.4).
type Testset struct {
	Verified  bool
	ACCovered bool
	Sensitive bool
	Approved  bool
	// TreeHash is the tree hash of tests.dir/KEY that was verified and pinned.
	TreeHash string
}

// ReviewRequest is one reviewer the change asked for. Covered is decided by the caller: the reviewer's last review
// was on a commit whose diff to head touches none of the files it reviewed.
type ReviewRequest struct {
	Reviewer string
	Covered  bool
	Waived   bool
}

// Approval is one review on the pull request. Only an approved review counts, and only if it is on head.
type Approval struct {
	Login    string
	Commit   id.SHA
	Approved bool
}

// Facts is everything the rule reads. The caller fills it from GitHub, the tests and the findings. A fact the
// caller does not have is left zero, and a zero fact fails the clause that needs it: silence never helps.
type Facts struct {
	AuthorLogin  string
	HeadSHA      id.SHA
	TicketLinked bool
	Slop         slop.Result

	Testset          Testset
	HeadTestTreeHash string

	// TestDiffKnown says the diff of the tests directory against the merge base was computed.
	TestDiffKnown bool
	// TestDiffOutsideKey lists changed test paths that are not under tests.dir/KEY.
	TestDiffOutsideKey []string

	// CIRequired are the ci_required checks from the policy; CheckAcceptance is always required as well.
	CIRequired []string
	CI         map[string]string

	ReviewsKnown   bool
	ReviewRequests []ReviewRequest

	// BlockingKnown says the blocking set and the open human cases were computed.
	BlockingKnown bool
	Blocking      []id.FindingID
	HumanPending  []id.CaseID

	Approvals []Approval
	// ApproversForChangedDomains are the logins the policy allows to approve the domains of the changed paths.
	ApproversForChangedDomains []string

	// PolicyKnown says both policies were read: at the merge base and at head.
	PolicyKnown         bool
	PolicyAtBase        policy.Policy
	PolicyAtHead        policy.Policy
	PolicyOwnerApproved bool
}

// Clause is one clause that is not met, with the reason a person can act on.
type Clause struct {
	ID     ClauseID
	Reason string
}

// Decision is the outcome. OK is true exactly when no clause is unmet.
type Decision struct {
	OK    bool
	Unmet []Clause
}

// clause pairs an id with the check that returns its reason, or "" when the clause holds.
type clause struct {
	id    ClauseID
	check func(Facts) string
}

// allClauses lists the ten clauses in the order TECH_SPEC 12.4 states them.
func allClauses() []clause {
	return []clause{
		{ClauseTicketLinked, checkTicket},
		{ClauseSlopPassed, checkSlop},
		{ClauseTestset, checkTestset},
		{ClauseTestTreePinned, checkTreePinned},
		{ClauseTestDiffScoped, checkTestDiff},
		{ClauseCI, checkCI},
		{ClauseReviews, checkReviews},
		{ClauseBlocking, checkBlocking},
		{ClauseApproval, checkApproval},
		{ClausePolicy, checkPolicy},
	}
}

// Evaluate applies the ten clauses. It lists every unmet clause, not only the first, so the check summary can say
// exactly what is missing.
func Evaluate(f Facts) Decision {
	var unmet []Clause
	for _, c := range allClauses() {
		if reason := c.check(f); reason != "" {
			unmet = append(unmet, Clause{ID: c.id, Reason: reason})
		}
	}
	return Decision{OK: len(unmet) == 0, Unmet: unmet}
}

func checkTicket(f Facts) string {
	if !f.TicketLinked {
		return "no ticket is linked to this pull request"
	}
	return ""
}

func checkSlop(f Facts) string {
	if f.Slop.Pass {
		return ""
	}
	if len(f.Slop.Failures) == 0 {
		return "the slop rule has not been evaluated for this head"
	}
	msgs := make([]string, 0, len(f.Slop.Failures))
	for _, fl := range f.Slop.Failures {
		msgs = append(msgs, fmt.Sprintf("%s: %s", fl.Rule, fl.Message))
	}
	return strings.Join(msgs, "; ")
}

func checkTestset(f Facts) string {
	var parts []string
	if !f.Testset.Verified {
		parts = append(parts, "the tests are not verified")
	}
	if !f.Testset.ACCovered {
		parts = append(parts, "not every acceptance criterion is covered by a test")
	}
	if f.Testset.Sensitive && !f.Testset.Approved {
		parts = append(parts, "the tests cover a sensitive domain and no domain approver has approved them")
	}
	return strings.Join(parts, "; ")
}

func checkTreePinned(f Facts) string {
	switch {
	case f.HeadTestTreeHash == "":
		return "the test tree hash at head is not known"
	case f.Testset.TreeHash == "":
		return "no test tree hash has been pinned for this change"
	case f.HeadTestTreeHash != f.Testset.TreeHash:
		return fmt.Sprintf("the test tree at head is %s but the pinned test set is %s", f.HeadTestTreeHash, f.Testset.TreeHash)
	}
	return ""
}

func checkTestDiff(f Facts) string {
	if !f.TestDiffKnown {
		return "the diff of the tests directory is not known"
	}
	if len(f.TestDiffOutsideKey) > 0 {
		return "the change edits tests outside its own key: " + strings.Join(f.TestDiffOutsideKey, ", ")
	}
	return ""
}

// checkCI requires the acceptance run and every ci_required check to succeed on head. A missing check is not success.
func checkCI(f Facts) string {
	var problems []string
	for _, name := range append([]string{CheckAcceptance}, f.CIRequired...) {
		if got, ok := f.CI[name]; !ok {
			problems = append(problems, fmt.Sprintf("check %s has not reported on head", name))
		} else if got != "success" {
			problems = append(problems, fmt.Sprintf("check %s is %s on head, want success", name, got))
		}
	}
	return strings.Join(problems, "; ")
}

func checkReviews(f Facts) string {
	if !f.ReviewsKnown {
		return "the review requests are not known"
	}
	var problems []string
	for _, r := range f.ReviewRequests {
		if !r.Covered && !r.Waived {
			problems = append(problems, fmt.Sprintf("review requested from %s is not covered on head and not waived", r.Reviewer))
		}
	}
	return strings.Join(problems, "; ")
}

func checkBlocking(f Facts) string {
	if !f.BlockingKnown {
		return "the blocking findings and open human cases are not known"
	}
	var parts []string
	if len(f.Blocking) > 0 {
		parts = append(parts, fmt.Sprintf("%d blocking finding(s) are open: %s", len(f.Blocking), joinIDs(f.Blocking)))
	}
	if len(f.HumanPending) > 0 {
		parts = append(parts, fmt.Sprintf("%d case(s) await a human: %s", len(f.HumanPending), joinCaseIDs(f.HumanPending)))
	}
	return strings.Join(parts, "; ")
}

// checkApproval holds when one approval satisfies all four invariant conditions. Otherwise the reasons for the
// approvals that were recorded are listed, so a person sees why the approval they gave did not count.
func checkApproval(f Facts) string {
	if len(f.Approvals) == 0 {
		return "no approval has been recorded from an approver of the changed domains"
	}
	var problems []string
	for _, a := range f.Approvals {
		p := approvalProblem(a, f)
		if p == "" {
			return ""
		}
		problems = append(problems, p)
	}
	return strings.Join(problems, "; ")
}

// approvalProblem says why an approval does not count, or returns "" when it does. The conditions are the invariant
// of TECH_SPEC 3.4: the approval is on head, it is not from the author, and its login is an approver for the domains.
func approvalProblem(a Approval, f Facts) string {
	switch {
	case !a.Approved:
		return fmt.Sprintf("review by %s is not an approval", a.Login)
	case f.HeadSHA == "":
		return "the head commit is not known"
	case a.Commit != f.HeadSHA:
		return fmt.Sprintf("approval by %s is for commit %s but head is %s", a.Login, a.Commit, f.HeadSHA)
	case f.AuthorLogin == "":
		return "the pull request author is not known"
	case a.Login == f.AuthorLogin:
		return fmt.Sprintf("approval by %s is from the author of the change", a.Login)
	case !slices.Contains(f.ApproversForChangedDomains, a.Login):
		return fmt.Sprintf("%s is not an approver for the domains of the changed paths", a.Login)
	}
	return ""
}

// checkPolicy holds when the policy did not change, or a policy owner approved the change to it (TECH_SPEC 12.4).
func checkPolicy(f Facts) string {
	if !f.PolicyKnown {
		return "the policy at the merge base and at head is not known"
	}
	if policy.Equal(f.PolicyAtBase, f.PolicyAtHead) || f.PolicyOwnerApproved {
		return ""
	}
	return "the policy file changed in this pull request and no policy owner approved the change"
}

func joinIDs(ids []id.FindingID) string {
	s := make([]string, 0, len(ids))
	for _, v := range ids {
		s = append(s, v.String())
	}
	return strings.Join(s, ", ")
}

func joinCaseIDs(ids []id.CaseID) string {
	s := make([]string, 0, len(ids))
	for _, v := range ids {
		s = append(s, v.String())
	}
	return strings.Join(s, ", ")
}
