package gateuse

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/slop"
)

// ErrNoPullRequest is returned when a change has no pr.opened or pr.pushed event yet.
var ErrNoPullRequest = errors.New("change has no pull request")

// ErrPullRequestClosed is returned when the change's pull request has closed.
var ErrPullRequestClosed = errors.New("change's pull request is closed")

// Outside holds the facts the event catalog does not carry. The caller reads them from the repository and the
// policy; Fold cannot derive them from events.
type Outside struct {
	// HeadTestTreeHash is the git tree hash of the change's test directory at the pull request head, read from the
	// repository. It is never derived from events: the pin clause exists to catch a PR that changed the pinned tests.
	HeadTestTreeHash           string
	TestDiffKnown              bool
	TestDiffOutsideKey         []string
	CIRequired                 []string
	ApproversForChangedDomains []string
	PolicyKnown                bool
	PolicyAtBase               policy.Policy
	PolicyAtHead               policy.Policy
	PolicyOwnerApproved        bool
	PolicyVersion              int
}

// Request names the change whose outside facts are read.
type Request struct {
	ChangeID id.ChangeID
	Repo     change.Repo
	Number   int
	HeadSHA  id.SHA
	Author   change.ActorID
}

// Source supplies the facts that are not in the event log.
type Source interface {
	Outside(ctx context.Context, req Request) (Outside, error)
}

// folded is the pure result of the event fold plus what Evaluate needs for the append.
type folded struct {
	facts         gate.Facts
	states        []gate.FindingState
	repo          change.Repo
	number        int
	closed        bool
	slopSeen      bool
	verifiedSeen  bool
	committedSeen bool
}

// pending reports whether the tests or the slop rule have not reported yet. Such a check is in_progress, not red.
func (f folded) pending() bool {
	return !f.slopSeen || !f.verifiedSeen || !f.committedSeen
}

// Fold turns the events of one change into the facts the pure merge rule reads. It returns ErrNoPullRequest when the
// stream has no pull request.
func Fold(events []change.Event) (gate.Facts, error) {
	f, err := foldEvents(events)
	if err != nil {
		return gate.Facts{}, err
	}
	return f.facts, nil
}

// foldEvents folds every event in version order and builds the facts, or refuses a stream without a pull request.
func foldEvents(events []change.Event) (folded, error) {
	st := newFoldState()
	for _, e := range events {
		if err := st.apply(e); err != nil {
			return folded{}, fmt.Errorf("fold %s at version %d: %w", e.Type, e.Version, err)
		}
	}
	if !st.hasPR {
		return folded{}, ErrNoPullRequest
	}
	return st.result(), nil
}

// ciResult is one check's last conclusion, with the commit it reported on, so an old head cannot count.
type ciResult struct {
	sha        id.SHA
	conclusion change.CIConclusion
}

// foldState accumulates the event facts. It is local to one Fold call.
type foldState struct {
	repo         change.Repo
	number       int
	hasPR        bool
	head         id.SHA
	author       change.ActorID
	ticketLinked bool
	closed       bool

	slop     slop.Result
	slopSeen bool

	acCovered     bool
	verified      bool
	verifiedTree  id.SHA
	sensitive     bool
	approved      bool
	pinned        id.SHA
	verifiedSeen  bool
	committedSeen bool

	ci        map[string]ciResult
	approvals []gate.Approval

	reviewsKnown bool
	reviews      []gate.ReviewRequest
	waived       map[string]bool

	findings     []id.FindingID
	findingState map[id.FindingID]gate.FindingState

	human map[id.CaseID]bool
}

func newFoldState() *foldState {
	return &foldState{
		ci:           make(map[string]ciResult),
		waived:       make(map[string]bool),
		findingState: make(map[id.FindingID]gate.FindingState),
		human:        make(map[id.CaseID]bool),
	}
}

// apply folds one event. An event that carries no fact the gate reads is ignored.
func (st *foldState) apply(e change.Event) error {
	p, err := change.Decode(e)
	if err != nil {
		return err
	}
	if handled, err := st.applyPR(p); handled {
		return err
	}
	if st.applyTests(p) || st.applyReview(p) {
		return nil
	}
	st.applyFindings(p)
	return nil
}

func (st *foldState) applyPR(p change.Payload) (bool, error) {
	switch v := p.(type) {
	case change.PROpened:
		return true, st.opened(v)
	case change.PRPushed:
		return true, st.pushed(v)
	case change.PRLinked:
		st.ticketLinked = true
		return true, nil
	case change.PRClosed:
		return true, st.closedPR(v)
	case change.Merged:
		st.closed = true
		return true, nil
	default:
		return false, nil
	}
}

func (st *foldState) applyTests(p change.Payload) bool {
	switch v := p.(type) {
	case change.SlopEvaluated:
		st.slop = slopResult(v)
		st.slopSeen = true
	case change.TestsSubmitted:
		st.acCovered = len(v.ACMap) > 0
	case change.TestsVerified:
		st.verifiedEvent(v)
	case change.TestsRejected:
		st.verified = false
		st.approved = false
	case change.TestsCommitted:
		st.committed(v)
	case change.TestsApproved:
		st.testsApproved(v)
	default:
		return false
	}
	return true
}

func (st *foldState) applyReview(p change.Payload) bool {
	switch v := p.(type) {
	case change.CIReported:
		if v.SHA == st.head {
			st.ci[v.Check] = ciResult{sha: v.SHA, conclusion: v.Conclusion}
		}
	case change.ApprovalRecorded:
		st.recordApproval(v)
	case change.ApprovalInvalidated:
		st.invalidate(v)
	case change.ReviewersPicked:
		st.pickReviewers(v)
	case change.ReviewWaived:
		st.waive(v)
	default:
		return false
	}
	return true
}

func (st *foldState) applyFindings(p change.Payload) {
	switch v := p.(type) {
	case change.FindingFiled:
		st.finding(change.FindingRecord(v))
	case change.FindingVerified:
		st.finding(change.FindingRecord(v))
	case change.FindingStateChanged:
		st.finding(change.FindingRecord(v))
	case change.CaseOpened:
		st.human[v.CaseID] = true
	case change.VerdictEscalated:
		st.human[v.CaseID] = true
	case change.VerdictApplied:
		delete(st.human, v.CaseID)
	}
}

// setPR keeps one pull request per change. A second one is refused rather than guessed at.
func (st *foldState) setPR(repo change.Repo, n int) error {
	if st.hasPR && (st.repo != repo || st.number != n) {
		return fmt.Errorf("change names pull request %s#%d and %s#%d", st.repo, st.number, repo, n)
	}
	st.repo, st.number, st.hasPR = repo, n, true
	return nil
}

func (st *foldState) opened(p change.PROpened) error {
	if err := st.setPR(p.Repo, p.Number); err != nil {
		return err
	}
	st.head = p.HeadSHA
	st.author = p.Author
	if p.Ticket != "" {
		st.ticketLinked = true
	}
	st.closed = false
	st.approvals = nil
	st.ci = make(map[string]ciResult)
	return nil
}

func (st *foldState) pushed(p change.PRPushed) error {
	if err := st.setPR(p.Repo, p.Number); err != nil {
		return err
	}
	st.head = p.HeadSHA
	st.approvals = nil
	st.ci = make(map[string]ciResult)
	return nil
}

func (st *foldState) closedPR(p change.PRClosed) error {
	if err := st.setPR(p.Repo, p.Number); err != nil {
		return err
	}
	st.closed = true
	return nil
}

// slopResult turns the event's rule names into the pure rule's failure list. The event does not carry the messages, so
// the message names the fact: the rule failed on this head.
func slopResult(p change.SlopEvaluated) slop.Result {
	r := slop.Result{Pass: p.Pass}
	if p.Pass {
		return r
	}
	r.Failures = make([]slop.Failure, 0, len(p.FailingRules))
	for _, name := range p.FailingRules {
		r.Failures = append(r.Failures, slop.Failure{
			Rule:    slop.Rule(name),
			Message: "the slop rule failed on this head",
		})
	}
	return r
}

func (st *foldState) verifiedEvent(p change.TestsVerified) {
	if !st.verified || p.TreeHash != st.verifiedTree || p.NeedsSignoff != st.sensitive {
		st.approved = false
	}
	st.verified = true
	st.verifiedSeen = true
	st.verifiedTree = p.TreeHash
	st.sensitive = p.NeedsSignoff
}

func (st *foldState) committed(p change.TestsCommitted) {
	st.pinned = p.TreeHash
	st.committedSeen = true
}

func (st *foldState) testsApproved(p change.TestsApproved) {
	if st.verified && p.TreeHash == st.verifiedTree {
		st.approved = true
	}
}

func (st *foldState) recordApproval(p change.ApprovalRecorded) {
	if !st.hasPR || p.CommitSHA != st.head || strings.EqualFold(string(p.Login), string(st.author)) {
		return
	}
	for _, a := range st.approvals {
		if strings.EqualFold(a.Login, string(p.Login)) && a.Commit == p.CommitSHA {
			return
		}
	}
	st.approvals = append(st.approvals, gate.Approval{
		Login: string(p.Login), Commit: p.CommitSHA, Approved: true,
	})
}

func (st *foldState) invalidate(p change.ApprovalInvalidated) {
	out := st.approvals[:0]
	for _, a := range st.approvals {
		if strings.EqualFold(a.Login, string(p.Login)) && a.Commit == p.CommitSHA {
			continue
		}
		out = append(out, a)
	}
	st.approvals = out
}

func (st *foldState) pickReviewers(p change.ReviewersPicked) {
	st.reviewsKnown = true
	st.reviews = make([]gate.ReviewRequest, 0, len(p.People))
	for _, person := range p.People {
		name := string(person)
		st.reviews = append(st.reviews, gate.ReviewRequest{Reviewer: name, Waived: st.waived[name]})
	}
}

func (st *foldState) waive(p change.ReviewWaived) {
	name := string(p.Reviewer)
	st.waived[name] = true
	for i := range st.reviews {
		if strings.EqualFold(st.reviews[i].Reviewer, name) {
			st.reviews[i].Waived = true
		}
	}
}

// finding records the latest state of a finding. The event carries no severity, so the severity is treated as
// critical: a finding can never pass a policy threshold because a field is missing.
func (st *foldState) finding(r change.FindingRecord) {
	if _, ok := st.findingState[r.FindingID]; !ok {
		st.findings = append(st.findings, r.FindingID)
	}
	st.findingState[r.FindingID] = gate.FindingState{
		ID:       r.FindingID,
		Tier:     r.Tier,
		Severity: finding.SeverityCritical,
		State:    r.State,
	}
}

// result builds the facts and the append metadata. Every fact that is not in the log stays zero, so the pure rule
// fails it closed; Evaluate adds the Outside facts before it calls the rule.
func (st *foldState) result() folded {
	states := make([]gate.FindingState, 0, len(st.findings))
	for _, id := range st.findings {
		states = append(states, st.findingState[id])
	}
	blocking := gate.BlockingSet(states, strictBlocking())

	human := make([]id.CaseID, 0, len(st.human))
	for c := range st.human {
		human = append(human, c)
	}
	slices.Sort(human)

	ci := make(map[string]string, len(st.ci))
	for name, r := range st.ci {
		if r.sha == st.head {
			ci[name] = string(r.conclusion)
		}
	}

	facts := gate.Facts{
		AuthorLogin:    string(st.author),
		HeadSHA:        st.head,
		TicketLinked:   st.ticketLinked,
		Slop:           st.slop,
		Testset:        gate.Testset{Verified: st.verified && st.committedSeen, ACCovered: st.acCovered, Sensitive: st.sensitive, Approved: st.approved, TreeHash: string(st.pinned)},
		CI:             ci,
		ReviewsKnown:   st.reviewsKnown && len(st.reviews) > 0,
		ReviewRequests: st.reviews,
		BlockingKnown:  true,
		Blocking:       blocking,
		HumanPending:   human,
		Approvals:      st.approvals,
	}
	return folded{
		facts:         facts,
		states:        states,
		repo:          st.repo,
		number:        st.number,
		closed:        st.closed,
		slopSeen:      st.slopSeen,
		verifiedSeen:  st.verifiedSeen,
		committedSeen: st.committedSeen,
	}
}

// strictBlocking is the threshold the fold uses when no policy is known: every open finding of tier E1 to E3 blocks. A
// finding event carries no severity, so severity is taken as critical and only the policy's tier threshold can lift a
// finding out of the blocking set.
func strictBlocking() policy.Blocking {
	return policy.Blocking{MinSeverity: finding.SeverityCritical, MinTier: finding.TierE4}
}

// blockingFor returns the blocking thresholds of the policy at the merge base (invariant 5), or the strict default
// when the policy is not known. A policy with a threshold the validator would refuse (tier E4) falls back to strict.
func blockingFor(o Outside) policy.Blocking {
	b := o.PolicyAtBase.Blocking
	if !o.PolicyKnown || b.MinTier == "" || b.MinTier == finding.TierE4 || b.MinSeverity == "" {
		return strictBlocking()
	}
	return b
}

// applyOutside copies the caller's facts onto the event facts. It never fills an event fact.
func applyOutside(f gate.Facts, o Outside) gate.Facts {
	f.HeadTestTreeHash = o.HeadTestTreeHash
	f.TestDiffKnown = o.TestDiffKnown
	f.TestDiffOutsideKey = o.TestDiffOutsideKey
	f.CIRequired = o.CIRequired
	f.ApproversForChangedDomains = o.ApproversForChangedDomains
	f.PolicyKnown = o.PolicyKnown
	f.PolicyAtBase = o.PolicyAtBase
	f.PolicyAtHead = o.PolicyAtHead
	f.PolicyOwnerApproved = o.PolicyOwnerApproved
	return f
}

// policyVersion is the version the event and the effect key name. A source that does not know it cannot make the
// version zero: the event requires 1 or more, and the gate is red anyway when the policy is unknown.
func policyVersion(o Outside) int {
	if o.PolicyVersion >= 1 {
		return o.PolicyVersion
	}
	if o.PolicyAtHead.Version >= 1 {
		return o.PolicyAtHead.Version
	}
	return 1
}

// eventTreeHash is the tree the event records. It is the pinned test tree when there is one, the verified test tree
// next, and the head commit last: the event needs a git hash even when the tests are not pinned yet.
func eventTreeHash(f gate.Facts) id.SHA {
	for _, s := range []string{f.Testset.TreeHash, f.HeadTestTreeHash, string(f.HeadSHA)} {
		if _, err := id.ParseSHA(s); err == nil {
			return id.SHA(s)
		}
	}
	return ""
}

// clip shortens s to at most limit bytes on a rune boundary, with an ellipsis inside the bound.
func clip(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit - 3
	if cut < 0 {
		cut = 0
	}
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}
