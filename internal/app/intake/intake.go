package intake

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/slop"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// gateCheck is the name of the required check run (TECH_SPEC 13).
const gateCheck = "heimdall/gate"

// ruleInvalidPolicy names the failing rule when the policy at the merge base cannot be used (fail closed).
const ruleInvalidPolicy = "policy_invalid"

// maxRetries is how many times a version conflict is retried after the first attempt (TECH_SPEC 2.4).
const maxRetries = 5

// intakeActor is the system actor of the slop verdict.
const intakeActor change.ActorID = "intake"

// ErrNoChange is returned when a pull request names a ticket that has no change yet. Such a PR is not unlinked: it
// waits for the ticket's change, so nothing is appended.
var ErrNoChange = errors.New("no change exists for the ticket")

// ErrSecondPR is returned when a pull request names a ticket whose change is already past attachment (it has a PR in
// a PR state), so the reducer refuses another pr.opened. Nothing is appended.
var ErrSecondPR = errors.New("second pull request for a ticket whose change already has one")

// Config holds the ports the use case needs. Every field is required.
type Config struct {
	Events ports.EventStore
	PRs    ports.PRReader
	Code   ports.CodeReader
	Clock  ports.Clock
	IDs    ports.IDs
	Policy PolicyDecoder
}

// Intake handles pull requests. Build it with New.
type Intake struct {
	events ports.EventStore
	prs    ports.PRReader
	code   ports.CodeReader
	clock  ports.Clock
	ids    ports.IDs
	policy PolicyDecoder
}

// Result says what one HandlePR call did. Pass is meaningful only when Appended is true.
type Result struct {
	Stream   id.ChangeID
	Appended bool
	Pass     bool
	Version  int64
}

// New checks the configuration and builds an Intake. It refuses a nil port, naming each one.
func New(cfg Config) (*Intake, error) {
	var missing []string
	if cfg.Events == nil {
		missing = append(missing, "Events")
	}
	if cfg.PRs == nil {
		missing = append(missing, "PRs")
	}
	if cfg.Code == nil {
		missing = append(missing, "Code")
	}
	if cfg.Clock == nil {
		missing = append(missing, "Clock")
	}
	if cfg.IDs == nil {
		missing = append(missing, "IDs")
	}
	if cfg.Policy == nil {
		missing = append(missing, "Policy")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("intake: nil port(s): %s", strings.Join(missing, ", "))
	}
	return &Intake{events: cfg.Events, prs: cfg.PRs, code: cfg.Code, clock: cfg.Clock, ids: cfg.IDs, policy: cfg.Policy}, nil
}

// verdict is the slop outcome of one handling: whether the rule passed, the failing rule ids (nil on pass), and the
// plain-language summary that the check run shows.
type verdict struct {
	pass    bool
	rules   []string
	summary string
}

// HandlePR reads pull request n of repo and appends what it saw. See the package doc for the order of events and the
// effect it writes. A nil error with Appended false means the head was already handled.
func (in *Intake) HandlePR(ctx context.Context, repo change.Repo, n int) (Result, error) {
	pr, err := in.prs.PR(ctx, repo, n)
	if err != nil {
		return Result{}, fmt.Errorf("read pull request %s#%d: %w", repo, n, err)
	}
	if err := pr.Validate(); err != nil {
		return Result{}, fmt.Errorf("pull request %s#%d: %w", repo, n, err)
	}
	if pr.Repo != repo || pr.Number != n {
		return Result{}, fmt.Errorf("pull request %s#%d reported as %s#%d", repo, n, pr.Repo, pr.Number)
	}
	files, err := in.prs.Files(ctx, repo, n)
	if err != nil {
		return Result{}, fmt.Errorf("read files of pull request %s#%d: %w", repo, n, err)
	}

	ticket := ticketKey(pr.Branch, pr.Title)
	stream, err := streamFor(repo, n, ticket)
	if err != nil {
		return Result{}, err
	}
	v, err := in.judge(ctx, repo, pr, files, ticket)
	if err != nil {
		return Result{}, err
	}

	j := job{repo: repo, pr: pr, ticket: ticket, stream: stream, v: v}
	for attempt := 0; ; attempt++ {
		res, err := in.appendOnce(ctx, j)
		if !errors.Is(err, ports.ErrVersionConflict) {
			return res, err
		}
		if attempt == maxRetries {
			return Result{}, fmt.Errorf("handle pull request %s#%d: still in conflict after %d attempts: %w",
				repo, n, maxRetries+1, ports.ErrVersionConflict)
		}
	}
}

// streamFor returns the change a pull request belongs to: its ticket's change, or its own stream when it names none
// (D146).
func streamFor(repo change.Repo, n int, ticket id.ChangeID) (id.ChangeID, error) {
	if ticket != "" {
		return ticket, nil
	}
	stream, err := id.PRChangeID(string(repo), n)
	if err != nil {
		return "", fmt.Errorf("stream for pull request %s#%d: %w", repo, n, err)
	}
	return stream, nil
}

// judge reads the policy at the base sha and, if it is usable, evaluates the slop rule on the head. An unusable policy
// fails closed without evaluating the rule.
func (in *Intake) judge(ctx context.Context, repo change.Repo, pr ports.PR, files []ports.ChangedFile, ticket id.ChangeID) (verdict, error) {
	read, err := readPolicy(ctx, in.code, in.policy, repo, pr.BaseSHA)
	if err != nil {
		return verdict{}, err
	}
	if read.invalid != "" {
		detail := ruleInvalidPolicy + ": " + read.invalid
		return failed([]string{detail}, detail), nil
	}
	facts, err := in.facts(ctx, repo, pr, files, ticket, read.pol.Tests.Dir)
	if err != nil {
		return verdict{}, err
	}
	res := slop.Evaluate(facts, read.pol.Slop, read.pol.Tests.Dir)
	if res.Pass {
		return verdict{pass: true, summary: "Slop rule passed. The merge rule is not evaluated yet, so the gate stays pending."}, nil
	}
	rules := make([]string, 0, len(res.Failures))
	msgs := make([]string, 0, len(res.Failures))
	for _, f := range res.Failures {
		rules = append(rules, string(f.Rule))
		msgs = append(msgs, fmt.Sprintf("%s: %s", f.Rule, f.Message))
	}
	return failed(rules, strings.Join(msgs, " ")), nil
}

// failed builds a failing verdict. The summary is the text the check run shows.
func failed(rules []string, detail string) verdict {
	return verdict{pass: false, rules: rules, summary: "Slop rule failed. " + detail}
}

// facts builds what the slop rule reads. ExpectedPaths stays nil: the test author's submission is not loadable through
// the ports yet, so every change outside the tests directory needs a justification (see doc.go).
func (in *Intake) facts(ctx context.Context, repo change.Repo, pr ports.PR, files []ports.ChangedFile, ticket id.ChangeID, testsDir string) (slop.Facts, error) {
	f := slop.Facts{Title: pr.Title, Branch: pr.Branch, Body: pr.Body, TicketKey: string(ticket)}
	for _, cf := range files {
		f.Files = append(f.Files, slop.File{Path: cf.Path, Added: cf.Additions, Deleted: cf.Deletions})
		if inDir(cf.Path, testsDir) {
			f.TestsChanged = true
		}
	}
	if !f.TestsChanged && ticket != "" {
		exists, err := in.heimdallTestsExist(ctx, repo, pr.HeadSHA, testsDir+"/"+string(ticket))
		if err != nil {
			return slop.Facts{}, err
		}
		f.HeimdallTestsExist = exists
	}
	return f, nil
}

// heimdallTestsExist reports whether the tests directory of the ticket exists at the head. ErrNotFound means none.
func (in *Intake) heimdallTestsExist(ctx context.Context, repo change.Repo, head id.SHA, dir string) (bool, error) {
	_, err := in.code.TreeHash(ctx, repo, head, dir)
	if errors.Is(err, ports.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("tests directory %s at %s: %w", dir, head, err)
	}
	return true, nil
}

// inDir reports whether path is dir or inside it. It mirrors the rule in internal/domain/slop, which is unexported.
func inDir(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, dir+"/")
}

// job is one handling of a pull request whose facts are fixed. appendOnce runs once per attempt.
type job struct {
	repo   change.Repo
	pr     ports.PR
	ticket id.ChangeID
	stream id.ChangeID
	v      verdict
}

// streamView is the stream as one attempt loaded it: its state, its last version, and the last head this PR was
// recorded with.
type streamView struct {
	state    change.State
	expected int64
	head     id.SHA
	seen     bool
}

// load reads the stream of a job and replays it. A ticket with no events has no change yet (ErrNoChange).
func (in *Intake) load(ctx context.Context, j job) (streamView, error) {
	existing, err := in.events.Load(ctx, j.stream)
	if err != nil {
		return streamView{}, fmt.Errorf("load %s: %w", j.stream, err)
	}
	if j.ticket != "" && len(existing) == 0 {
		return streamView{}, fmt.Errorf("ticket %s: %w", j.ticket, ErrNoChange)
	}
	state, err := change.Replay(existing)
	if err != nil {
		return streamView{}, fmt.Errorf("stream %s does not replay: %w", j.stream, err)
	}
	var v streamView
	v.state = state
	if len(existing) > 0 {
		v.expected = existing[len(existing)-1].Version
	}
	v.head, v.seen, err = lastHead(existing, j.repo, j.pr.Number)
	if err != nil {
		return streamView{}, fmt.Errorf("stream %s: %w", j.stream, err)
	}
	return v, nil
}

// appendOnce loads the stream, decides, and appends in one Append. It returns ports.ErrVersionConflict, wrapped,
// when another writer got there first, so HandlePR can try again.
func (in *Intake) appendOnce(ctx context.Context, j job) (Result, error) {
	v, err := in.load(ctx, j)
	if err != nil {
		return Result{}, err
	}
	if v.seen && v.head == j.pr.HeadSHA {
		return Result{Stream: j.stream}, nil
	}

	evs, err := in.buildEvents(j, v.expected, v.seen)
	if err != nil {
		return Result{}, err
	}
	if err := checkTransitions(v.state, evs, j, v.seen); err != nil {
		return Result{}, err
	}

	fx, err := in.gateEffect(j, v.expected+int64(len(evs)))
	if err != nil {
		return Result{}, err
	}
	ver, err := in.events.Append(ctx, j.stream, v.expected, evs, []change.Effect{fx})
	if err != nil {
		return Result{}, fmt.Errorf("append to %s: %w", j.stream, err)
	}
	return Result{Stream: j.stream, Appended: true, Pass: j.v.pass, Version: ver}, nil
}

// buildEvents returns the PR event (pr.opened the first time, pr.pushed when the head moved) and slop.evaluated, with
// versions after expected and one correlation id shared by the batch.
func (in *Intake) buildEvents(j job, expected int64, seen bool) ([]change.Event, error) {
	prActor := change.Actor{Kind: change.ActorGitHub, ID: j.pr.Author}
	var prType change.EventType
	var prPayload change.Payload
	if seen {
		prType = change.EventPRPushed
		prPayload = change.PRPushed{Repo: j.repo, Number: j.pr.Number, HeadSHA: j.pr.HeadSHA}
	} else {
		prType = change.EventPROpened
		prPayload = change.PROpened{Repo: j.repo, Number: j.pr.Number, HeadSHA: j.pr.HeadSHA, Author: j.pr.Author, Ticket: j.ticket}
	}
	slopPayload := change.SlopEvaluated{Pass: j.v.pass, FailingRules: j.v.rules}

	firstID := in.ids.NewEvent()
	corr := change.CorrelationID(firstID.String())
	first, err := in.event(firstID, corr, j.stream, expected+1, prType, prActor, prPayload)
	if err != nil {
		return nil, err
	}
	second, err := in.event(in.ids.NewEvent(), corr, j.stream, expected+2, change.EventSlopEvaluated,
		change.Actor{Kind: change.ActorSystem, ID: intakeActor}, slopPayload)
	if err != nil {
		return nil, err
	}
	return []change.Event{first, second}, nil
}

// event builds one envelope and checks its payload before it is stored.
func (in *Intake) event(eid change.EventID, corr change.CorrelationID, stream id.ChangeID, version int64,
	t change.EventType, actor change.Actor, p change.Payload) (change.Event, error) {
	if err := p.Validate(); err != nil {
		return change.Event{}, fmt.Errorf("payload of %s: %w", t, err)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return change.Event{}, fmt.Errorf("encode payload of %s: %w", t, err)
	}
	return change.Event{
		ID: eid, ChangeID: stream, Version: version, Type: t, Actor: actor,
		OccurredAt: in.clock.Now().UTC(), CorrelationID: corr, Payload: raw,
	}, nil
}

// checkTransitions runs the new events through the reducer from the state the stream is in. A second pr.opened on a
// ticket in a PR state is named as ErrSecondPR, not as a generic refusal.
func checkTransitions(state change.State, evs []change.Event, j job, seen bool) error {
	s := state
	for i, e := range evs {
		next, err := change.Reduce(s, e)
		if err != nil {
			if i == 0 && j.ticket != "" && !seen && !state.IsTerminal() && errors.Is(err, change.ErrIllegalTransition) {
				return fmt.Errorf("ticket %s is in %s: %w", j.ticket, state, ErrSecondPR)
			}
			return fmt.Errorf("stream %s: %w", j.stream, err)
		}
		s = next
	}
	return nil
}

// gateEffect builds the github_check row that shows the verdict on the head. Its key depends on the new version and
// the head, so a new head or a new version never reuses a key.
func (in *Intake) gateEffect(j job, newVersion int64) (change.Effect, error) {
	key, err := change.IdempotencyKey(j.stream, newVersion, change.EffectGitHubCheck,
		string(j.repo)+":"+j.pr.HeadSHA.String())
	if err != nil {
		return change.Effect{}, fmt.Errorf("gate effect key: %w", err)
	}
	// The shared github_check payload: the executor reads the same type, and the effect key is the check's external id.
	c := change.CheckPayload{
		Repo:    j.repo,
		SHA:     j.pr.HeadSHA,
		Name:    gateCheck,
		Summary: clipSummary(j.v.summary, change.MaxCheckSummary),
	}
	if j.v.pass {
		c.Status = change.CheckStatusInProgress
	} else {
		c.Status = change.CheckStatusCompleted
		c.Conclusion = change.CIFailure
	}
	if err := c.Validate(); err != nil {
		return change.Effect{}, fmt.Errorf("gate check: %w", err)
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return change.Effect{}, fmt.Errorf("encode gate check: %w", err)
	}
	return change.Effect{Key: key, Kind: change.EffectGitHubCheck, Target: string(j.repo), Payload: raw}, nil
}

// clipSummary cuts s to at most limit bytes on a rune boundary, with an ellipsis inside the limit.
func clipSummary(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit - 3
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}

// lastHead returns the head sha of the latest pr.opened or pr.pushed for pull request n of repo, and whether one exists.
func lastHead(events []change.Event, repo change.Repo, n int) (id.SHA, bool, error) {
	var head id.SHA
	seen := false
	for _, e := range events {
		if e.Type != change.EventPROpened && e.Type != change.EventPRPushed {
			continue
		}
		p, err := change.Decode(e)
		if err != nil {
			return "", false, fmt.Errorf("version %d: %w", e.Version, err)
		}
		switch v := p.(type) {
		case change.PROpened:
			if v.Repo == repo && v.Number == n {
				head, seen = v.HeadSHA, true
			}
		case change.PRPushed:
			if v.Repo == repo && v.Number == n {
				head, seen = v.HeadSHA, true
			}
		}
	}
	return head, seen, nil
}
