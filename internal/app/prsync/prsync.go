package prsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// maxRetries is the number of times a version conflict is retried (TECH_SPEC 2.4).
const maxRetries = 5

// githubActorID is the actor ID used for GitHub system-level events like PR closures and CI reports.
const githubActorID = change.ActorID("github")

var reKeyCandidate = regexp.MustCompile(`[A-Z][A-Z0-9_]+-[0-9]+`)

// ReviewState is the state of a GitHub pull request review.
type ReviewState string

// The review states.
const (
	ReviewApproved         ReviewState = "approved"
	ReviewChangesRequested ReviewState = "changes_requested"
	ReviewCommented        ReviewState = "commented"
	ReviewDismissed        ReviewState = "dismissed"
)

// PRReview is a pull request review reported by GitHub.
type PRReview struct {
	ID          string
	Login       change.ActorID
	State       ReviewState
	CommitSHA   id.SHA
	SubmittedAt time.Time
}

// CheckStatus is the status of a GitHub check run.
type CheckStatus string

// The check run statuses.
const (
	CheckQueued     CheckStatus = "queued"
	CheckInProgress CheckStatus = "in_progress"
	CheckCompleted  CheckStatus = "completed"
)

// CheckRun is one check run on a commit reported by GitHub.
type CheckRun struct {
	ID         string
	Name       string
	Status     CheckStatus
	Conclusion change.CIConclusion
	SHA        id.SHA
}

// Handled says what Intake did for one pull request. Appended is true when Intake appended pr.opened or pr.pushed,
// which it does when the head is not in the log yet. That is also the first sight of a head after a restart.
type Handled struct {
	Change   id.ChangeID
	Appended bool
}

// Intake processes an open pull request through slop and intake logic, returning the change it belongs to.
type Intake interface {
	HandlePR(ctx context.Context, repo change.Repo, n int) (Handled, error)
}

// Gate evaluates the merge rule for a change.
type Gate interface {
	Evaluate(ctx context.Context, changeID id.ChangeID) error
}

// Reviews reads submitted reviews of a pull request.
type Reviews interface {
	PRReviews(ctx context.Context, repo change.Repo, n int) ([]PRReview, error)
}

// Checks reads check runs on a commit.
type Checks interface {
	CheckRuns(ctx context.Context, repo change.Repo, sha id.SHA) ([]CheckRun, error)
}

// PRs lists open pull requests for a repository, and reads one pull request in any state.
type PRs interface {
	OpenPRs(ctx context.Context, repo change.Repo) ([]ports.PR, error)
	PR(ctx context.Context, repo change.Repo, n int) (ports.PR, error)
}

// FailedPR records a failure to sync one pull request.
type FailedPR struct {
	Repo   change.Repo
	Number int
	Err    error
}

// Result counts what one SyncOnce call did and lists any per-PR failures.
type Result struct {
	Synced    int
	Appended  int
	Evaluated int
	Failed    []FailedPR
}

// Config holds the configuration and ports Syncer needs. Every field is required.
type Config struct {
	Repos   []change.Repo
	PRs     PRs
	Streams ports.PRStreams
	Intake  Intake
	Reviews Reviews
	Checks  Checks
	Gate    Gate
	Events  ports.EventStore
	Clock   ports.Clock
	IDs     ports.IDs
}

// Syncer syncs open pull requests, reviews and check runs into change events, and runs the gate.
type Syncer struct {
	repos   []change.Repo
	prs     PRs
	streams ports.PRStreams
	intake  Intake
	reviews Reviews
	checks  Checks
	gate    Gate
	events  ports.EventStore
	clock   ports.Clock
	ids     ports.IDs

	seenReviews  map[string]struct{}
	lastHeads    map[id.ChangeID]id.SHA
	lastVersions map[id.ChangeID]int64
}

// New checks the configuration and builds a Syncer. It refuses missing or nil ports.
func New(cfg Config) (*Syncer, error) {
	var missing []string
	if len(cfg.Repos) == 0 {
		missing = append(missing, "Repos")
	}
	if cfg.PRs == nil {
		missing = append(missing, "PRs")
	}
	if cfg.Streams == nil {
		missing = append(missing, "Streams")
	}
	if cfg.Intake == nil {
		missing = append(missing, "Intake")
	}
	if cfg.Reviews == nil {
		missing = append(missing, "Reviews")
	}
	if cfg.Checks == nil {
		missing = append(missing, "Checks")
	}
	if cfg.Gate == nil {
		missing = append(missing, "Gate")
	}
	if cfg.Events == nil {
		missing = append(missing, "Events")
	}
	if cfg.Clock == nil {
		missing = append(missing, "Clock")
	}
	if cfg.IDs == nil {
		missing = append(missing, "IDs")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("prsync: nil port(s): %s", strings.Join(missing, ", "))
	}
	return &Syncer{
		repos:        cfg.Repos,
		prs:          cfg.PRs,
		streams:      cfg.Streams,
		intake:       cfg.Intake,
		reviews:      cfg.Reviews,
		checks:       cfg.Checks,
		gate:         cfg.Gate,
		events:       cfg.Events,
		clock:        cfg.Clock,
		ids:          cfg.IDs,
		seenReviews:  make(map[string]struct{}),
		lastHeads:    make(map[id.ChangeID]id.SHA),
		lastVersions: make(map[id.ChangeID]int64),
	}, nil
}

// Reconcile is the boot pass (TECH_SPEC 2.2, D56). It reads each pull request that the event log still holds as open,
// and appends pr.closed for each one that GitHub now reports closed: a PR merged or closed while the server was down
// never reaches SyncOnce, because OpenPRs lists open pull requests only. A pull request that is still open is left to
// SyncOnce. A failed read is named in Failed and the next pull request goes on. It is idempotent: a stream that has
// pr.closed already is skipped, so a restart or a second call appends nothing.
func (s *Syncer) Reconcile(ctx context.Context) (Result, error) {
	streams, err := s.streams.OpenPRStreams(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("list open pull request streams: %w", err)
	}
	var res Result
	for _, st := range streams {
		pr, err := s.prs.PR(ctx, st.Repo, st.Number)
		if err != nil {
			res.Failed = append(res.Failed, FailedPR{Repo: st.Repo, Number: st.Number,
				Err: fmt.Errorf("read pull request %s#%d: %w", st.Repo, st.Number, err)})
			continue
		}
		if pr.State != ports.PRStateClosed {
			continue
		}
		appended, err := s.appendClosedOnce(ctx, st.Change, st.Repo, pr)
		if err != nil {
			res.Failed = append(res.Failed, FailedPR{Repo: st.Repo, Number: st.Number, Err: err})
			continue
		}
		res.Appended += appended
	}
	return res, nil
}

// SyncOnce runs one synchronization cycle across all configured repositories.
func (s *Syncer) SyncOnce(ctx context.Context) (Result, error) {
	var res Result
	for _, repo := range s.repos {
		prs, err := s.prs.OpenPRs(ctx, repo)
		if err != nil {
			return res, fmt.Errorf("list open pull requests for %s: %w", repo, err)
		}
		for _, pr := range prs {
			if err := s.syncPR(ctx, repo, pr, &res); err != nil {
				res.Failed = append(res.Failed, FailedPR{Repo: repo, Number: pr.Number, Err: err})
			} else {
				res.Synced++
			}
		}
	}
	return res, nil
}

// intakeChange runs intake for a pull request. When intake fails for a closed PR, the change comes from the branch or
// title, so its pr.closed still lands on the right stream.
func (s *Syncer) intakeChange(ctx context.Context, repo change.Repo, pr ports.PR) (Handled, error) {
	handled, err := s.intake.HandlePR(ctx, repo, pr.Number)
	if err == nil {
		return handled, nil
	}
	if pr.State != ports.PRStateClosed {
		return Handled{}, fmt.Errorf("intake PR %s#%d: %w", repo, pr.Number, err)
	}
	derived, derr := changeIDForPR(pr)
	if derr != nil {
		return Handled{}, fmt.Errorf("intake PR %s#%d: %w", repo, pr.Number, err)
	}
	return Handled{Change: derived}, nil
}

// syncPR synchronizes a single pull request.
func (s *Syncer) syncPR(ctx context.Context, repo change.Repo, pr ports.PR, res *Result) error {
	handled, err := s.intakeChange(ctx, repo, pr)
	if err != nil {
		return err
	}
	changeID := handled.Change

	if pr.State == ports.PRStateClosed {
		appended, err := s.appendClosedOnce(ctx, changeID, repo, pr)
		if err != nil {
			return err
		}
		res.Appended += appended
		return nil
	}

	reviews, err := s.reviews.PRReviews(ctx, repo, pr.Number)
	if err != nil {
		return fmt.Errorf("read reviews for %s#%d: %w", repo, pr.Number, err)
	}

	checks, err := s.checks.CheckRuns(ctx, repo, pr.HeadSHA)
	if err != nil {
		return fmt.Errorf("read check runs for %s#%d at %s: %w", repo, pr.Number, pr.HeadSHA, err)
	}

	appended, headChanged, err := s.syncOpenPR(ctx, changeID, pr, reviews, checks)
	if err != nil {
		return err
	}
	res.Appended += appended

	// handled.Appended covers a head the log did not hold yet. After a restart headChanged is false, because the
	// in-memory head is gone, so intake's append is the only sign that the gate must run again.
	if appended > 0 || headChanged || handled.Appended {
		if err := s.gate.Evaluate(ctx, changeID); err != nil {
			return fmt.Errorf("evaluate gate for %s: %w", changeID, err)
		}
		res.Evaluated++
	}
	return nil
}

func hasClosedEvent(events []change.Event, repo change.Repo, number int) bool {
	for _, e := range events {
		if e.Type == change.EventPRClosed {
			var p change.PRClosed
			if err := json.Unmarshal(e.Payload, &p); err == nil && p.Repo == repo && p.Number == number {
				return true
			}
		}
	}
	return false
}

// appendClosedOnce ensures a closed pull request has exactly one pr.closed event appended.
func (s *Syncer) appendClosedOnce(ctx context.Context, changeID id.ChangeID, repo change.Repo, pr ports.PR) (int, error) {
	for attempt := 0; attempt <= maxRetries; attempt++ {
		events, err := s.events.Load(ctx, changeID)
		if err != nil {
			return 0, fmt.Errorf("load stream %s: %w", changeID, err)
		}
		if hasClosedEvent(events, repo, pr.Number) {
			return 0, nil
		}

		var expected int64
		if len(events) > 0 {
			expected = events[len(events)-1].Version
		}

		ev, err := s.buildClosedEvent(changeID, repo, pr, expected)
		if err != nil {
			return 0, err
		}

		_, err = s.events.Append(ctx, changeID, expected, []change.Event{ev}, nil)
		if errors.Is(err, ports.ErrVersionConflict) {
			if attempt < maxRetries {
				continue
			}
			return 0, fmt.Errorf("append pr.closed to %s: %w", changeID, err)
		}
		if err != nil {
			return 0, fmt.Errorf("append pr.closed to %s: %w", changeID, err)
		}
		return 1, nil
	}
	return 0, ports.ErrVersionConflict
}

func (s *Syncer) buildClosedEvent(changeID id.ChangeID, repo change.Repo, pr ports.PR, expected int64) (change.Event, error) {
	payload := change.PRClosed{
		Repo:    repo,
		Number:  pr.Number,
		HeadSHA: pr.HeadSHA,
		Merged:  pr.Merged,
	}
	if err := payload.Validate(); err != nil {
		return change.Event{}, fmt.Errorf("payload of pr.closed: %w", err)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return change.Event{}, fmt.Errorf("encode pr.closed: %w", err)
	}

	eid := s.ids.NewEvent()
	ev := change.Event{
		ID:            eid,
		ChangeID:      changeID,
		Version:       expected + 1,
		Type:          change.EventPRClosed,
		Actor:         change.Actor{Kind: change.ActorGitHub, ID: githubActorID},
		OccurredAt:    s.clock.Now().UTC(),
		CorrelationID: change.CorrelationID(eid.String()),
		Payload:       raw,
	}
	if err := ev.Validate(); err != nil {
		return change.Event{}, fmt.Errorf("pr.closed for %s: %w", changeID, err)
	}
	if _, err := change.Decode(ev); err != nil {
		return change.Event{}, fmt.Errorf("decode pr.closed for %s: %w", changeID, err)
	}
	return ev, nil
}

func (s *Syncer) checkHeadChanged(changeID id.ChangeID, currentHead id.SHA) bool {
	if prevHead, ok := s.lastHeads[changeID]; ok && prevHead != currentHead {
		return true
	}
	return false
}

func (s *Syncer) recordProgress(changeID id.ChangeID, currentHead id.SHA, version int64, seenReviews []string) {
	s.lastHeads[changeID] = currentHead
	s.lastVersions[changeID] = version
	for _, rID := range seenReviews {
		s.seenReviews[rID] = struct{}{}
	}
}

// syncOpenPR synchronizes reviews and check runs for an open pull request.
func (s *Syncer) syncOpenPR(ctx context.Context, changeID id.ChangeID, pr ports.PR, reviews []PRReview, checks []CheckRun) (int, bool, error) {
	for attempt := 0; attempt <= maxRetries; attempt++ {
		events, err := s.events.Load(ctx, changeID)
		if err != nil {
			return 0, false, fmt.Errorf("load stream %s: %w", changeID, err)
		}

		var expected int64
		if len(events) > 0 {
			expected = events[len(events)-1].Version
		}

		headChanged := s.checkHeadChanged(changeID, pr.HeadSHA)
		newEvents, newlySeenReviews, err := s.decide(changeID, pr, reviews, checks, events, expected)
		if err != nil {
			return 0, false, err
		}

		if len(newEvents) == 0 {
			s.recordProgress(changeID, pr.HeadSHA, expected, newlySeenReviews)
			return 0, headChanged, nil
		}

		_, err = s.events.Append(ctx, changeID, expected, newEvents, nil)
		if errors.Is(err, ports.ErrVersionConflict) {
			if attempt < maxRetries {
				continue
			}
			return 0, false, fmt.Errorf("append to %s: %w", changeID, err)
		}
		if err != nil {
			return 0, false, fmt.Errorf("append to %s: %w", changeID, err)
		}

		s.recordProgress(changeID, pr.HeadSHA, expected+int64(len(newEvents)), newlySeenReviews)
		return len(newEvents), headChanged, nil
	}
	return 0, false, ports.ErrVersionConflict
}

// decide computes the list of new events to append for reviews and check runs.
func (s *Syncer) decide(changeID id.ChangeID, pr ports.PR, reviews []PRReview, checks []CheckRun, events []change.Event, expected int64) ([]change.Event, []string, error) {
	revEvents, seen, err := s.decideReviews(changeID, pr, reviews, events, expected, 0)
	if err != nil {
		return nil, nil, err
	}
	ciEvents, err := s.decideChecks(changeID, pr, checks, events, expected, len(revEvents))
	if err != nil {
		return nil, nil, err
	}
	all := make([]change.Event, 0, len(revEvents)+len(ciEvents))
	all = append(all, revEvents...)
	all = append(all, ciEvents...)
	return all, seen, nil
}

func findActiveApprovals(events []change.Event) map[change.ActorID]id.SHA {
	active := make(map[change.ActorID]id.SHA)
	for _, e := range events {
		switch string(e.Type) {
		case string(change.EventPRPushed):
			active = make(map[change.ActorID]id.SHA)
		case string(change.EventApprovalRecorded):
			var p change.ApprovalRecorded
			if err := json.Unmarshal(e.Payload, &p); err == nil {
				active[p.Login] = p.CommitSHA
			}
		case string(change.EventApprovalInvalidated):
			var p change.ApprovalInvalidated
			if err := json.Unmarshal(e.Payload, &p); err == nil && active[p.Login] == p.CommitSHA {
				delete(active, p.Login)
			}
		}
	}
	return active
}

func (s *Syncer) decideReviews(changeID id.ChangeID, pr ports.PR, reviews []PRReview, events []change.Event, expected int64, offset int) ([]change.Event, []string, error) {
	active := findActiveApprovals(events)
	var evs []change.Event
	var seen []string

	for _, r := range reviews {
		if r.CommitSHA != pr.HeadSHA || strings.EqualFold(string(r.Login), string(pr.Author)) || s.isReviewSeen(reviewKey(r), events) {
			continue
		}
		seen = append(seen, reviewKey(r))
		version := expected + int64(offset+len(evs)+1)

		switch r.State {
		case ReviewApproved:
			ev, err := s.buildApprovalEvent(changeID, r, version)
			if err != nil {
				return nil, nil, err
			}
			evs = append(evs, ev)
			active[r.Login] = pr.HeadSHA
		case ReviewDismissed, ReviewChangesRequested:
			if sha, ok := active[r.Login]; ok && sha == pr.HeadSHA {
				ev, err := s.buildInvalidationEvent(changeID, r, version)
				if err != nil {
					return nil, nil, err
				}
				evs = append(evs, ev)
				delete(active, r.Login)
			}
		case ReviewCommented:
			// Comments do not affect approval status.
		default:
			// Other states ignored.
		}
	}
	return evs, seen, nil
}

func findLatestCI(events []change.Event) map[string]change.CIReported {
	latest := make(map[string]change.CIReported)
	for _, e := range events {
		if e.Type == change.EventCIReported {
			var p change.CIReported
			if err := json.Unmarshal(e.Payload, &p); err == nil {
				latest[p.Check] = p
			}
		}
	}
	return latest
}

func (s *Syncer) decideChecks(changeID id.ChangeID, pr ports.PR, checks []CheckRun, events []change.Event, expected int64, offset int) ([]change.Event, error) {
	latest := findLatestCI(events)
	var evs []change.Event

	for _, run := range checks {
		if run.SHA != pr.HeadSHA || run.Status != CheckCompleted || run.Conclusion == "" || !run.Conclusion.Valid() {
			continue
		}
		cur, exists := latest[run.Name]
		if exists && cur.SHA == run.SHA && cur.Conclusion == run.Conclusion {
			continue
		}

		version := expected + int64(offset+len(evs)+1)
		ev, err := s.buildCIEvent(changeID, run, version)
		if err != nil {
			return nil, err
		}
		evs = append(evs, ev)
		latest[run.Name] = change.CIReported{
			Check:      run.Name,
			SHA:        run.SHA,
			Conclusion: run.Conclusion,
		}
	}
	return evs, nil
}

// buildApprovalEvent constructs and validates an approval.recorded event.
func (s *Syncer) buildApprovalEvent(changeID id.ChangeID, r PRReview, version int64) (change.Event, error) {
	pl := change.ApprovalRecorded{Login: r.Login, CommitSHA: r.CommitSHA}
	if err := pl.Validate(); err != nil {
		return change.Event{}, fmt.Errorf("payload of approval.recorded: %w", err)
	}
	raw, err := json.Marshal(pl)
	if err != nil {
		return change.Event{}, fmt.Errorf("encode approval.recorded: %w", err)
	}

	occ := r.SubmittedAt.UTC()
	if occ.IsZero() {
		occ = s.clock.Now().UTC()
	}

	ev := change.Event{
		ID:            s.ids.NewEvent(),
		ChangeID:      changeID,
		Version:       version,
		Type:          change.EventApprovalRecorded,
		Actor:         change.Actor{Kind: change.ActorHuman, ID: r.Login},
		OccurredAt:    occ,
		CorrelationID: change.CorrelationID("rev-" + strings.ReplaceAll(reviewKey(r), " ", "_")),
		Payload:       raw,
	}
	if err := ev.Validate(); err != nil {
		return change.Event{}, fmt.Errorf("approval.recorded for %s: %w", changeID, err)
	}
	if _, err := change.Decode(ev); err != nil {
		return change.Event{}, fmt.Errorf("decode approval.recorded for %s: %w", changeID, err)
	}
	return ev, nil
}

// buildInvalidationEvent constructs and validates an approval.invalidated event.
func (s *Syncer) buildInvalidationEvent(changeID id.ChangeID, r PRReview, version int64) (change.Event, error) {
	pl := change.ApprovalInvalidated{Login: r.Login, CommitSHA: r.CommitSHA}
	if err := pl.Validate(); err != nil {
		return change.Event{}, fmt.Errorf("payload of approval.invalidated: %w", err)
	}
	raw, err := json.Marshal(pl)
	if err != nil {
		return change.Event{}, fmt.Errorf("encode approval.invalidated: %w", err)
	}

	occ := r.SubmittedAt.UTC()
	if occ.IsZero() {
		occ = s.clock.Now().UTC()
	}

	ev := change.Event{
		ID:            s.ids.NewEvent(),
		ChangeID:      changeID,
		Version:       version,
		Type:          change.EventApprovalInvalidated,
		Actor:         change.Actor{Kind: change.ActorHuman, ID: r.Login},
		OccurredAt:    occ,
		CorrelationID: change.CorrelationID("rev-" + strings.ReplaceAll(reviewKey(r), " ", "_")),
		Payload:       raw,
	}
	if err := ev.Validate(); err != nil {
		return change.Event{}, fmt.Errorf("approval.invalidated for %s: %w", changeID, err)
	}
	if _, err := change.Decode(ev); err != nil {
		return change.Event{}, fmt.Errorf("decode approval.invalidated for %s: %w", changeID, err)
	}
	return ev, nil
}

// buildCIEvent constructs and validates a ci.reported event.
func (s *Syncer) buildCIEvent(changeID id.ChangeID, run CheckRun, version int64) (change.Event, error) {
	pl := change.CIReported{
		Check:      run.Name,
		SHA:        run.SHA,
		Conclusion: run.Conclusion,
	}
	if err := pl.Validate(); err != nil {
		return change.Event{}, fmt.Errorf("payload of ci.reported: %w", err)
	}
	raw, err := json.Marshal(pl)
	if err != nil {
		return change.Event{}, fmt.Errorf("encode ci.reported: %w", err)
	}

	eid := s.ids.NewEvent()
	ev := change.Event{
		ID:            eid,
		ChangeID:      changeID,
		Version:       version,
		Type:          change.EventCIReported,
		Actor:         change.Actor{Kind: change.ActorGitHub, ID: githubActorID},
		OccurredAt:    s.clock.Now().UTC(),
		CorrelationID: change.CorrelationID(eid.String()),
		Payload:       raw,
	}
	if err := ev.Validate(); err != nil {
		return change.Event{}, fmt.Errorf("ci.reported for %s: %w", changeID, err)
	}
	if _, err := change.Decode(ev); err != nil {
		return change.Event{}, fmt.Errorf("decode ci.reported for %s: %w", changeID, err)
	}
	return ev, nil
}

// reviewKey identifies one state of one review. GitHub does not add a review when an approval is dismissed: the same
// review changes from approved to dismissed. Keying on the ID alone would hide that change forever.
func reviewKey(r PRReview) string {
	return r.ID + "-" + string(r.State)
}

// isReviewSeen reports whether a review key has already been recorded in memory or in stream events.
func (s *Syncer) isReviewSeen(reviewID string, events []change.Event) bool {
	if _, ok := s.seenReviews[reviewID]; ok {
		return true
	}
	tag := "rev-" + strings.ReplaceAll(reviewID, " ", "_")
	for _, e := range events {
		if string(e.CorrelationID) == tag {
			s.seenReviews[reviewID] = struct{}{}
			return true
		}
	}
	return false
}

// ticketKey returns the first ticket key in the branch, then in the title, or "" when neither names one.
func ticketKey(branch, title string) id.ChangeID {
	for _, text := range []string{branch, title} {
		if k := keyIn(text); k != "" {
			return k
		}
	}
	return ""
}

// keyIn returns the first candidate in text that is a whole token and a valid ticket key, or "".
func keyIn(text string) id.ChangeID {
	for _, loc := range reKeyCandidate.FindAllStringIndex(text, -1) {
		if !tokenEdge(text, loc[0]-1) || !tokenEdge(text, loc[1]) {
			continue
		}
		if k, err := id.ParseChangeID(text[loc[0]:loc[1]]); err == nil {
			return k
		}
	}
	return ""
}

// tokenEdge reports whether byte i of s lies outside a token: start or end of s, or non-alphanumeric.
func tokenEdge(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return true
	}
	switch c := s[i]; {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return false
	}
	return true
}

// changeIDForPR derives the change ID of a pull request from its branch/title or PR change id.
func changeIDForPR(pr ports.PR) (id.ChangeID, error) {
	ticket := ticketKey(pr.Branch, pr.Title)
	if ticket != "" {
		return ticket, nil
	}
	return id.PRChangeID(string(pr.Repo), pr.Number)
}
