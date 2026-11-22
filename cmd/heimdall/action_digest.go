package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/github"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/gitlog"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/policyyaml"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/digest"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// The digest step's own limits. A week of pull requests is far below them; the caps keep one run's work and its output
// bounded on a repository whose state ref is much older than the period.
const (
	// maxDigestChanges is the most change logs one run reads. A repository with more is read newest first, and the run
	// says how many it did not read.
	maxDigestChanges = 1000
	// maxSnapshotBytes bounds the JSON snapshot the -in form reads.
	maxSnapshotBytes = 8 << 20
	// maxTreeEntries bounds a recursive tree listing: the state ref's own tree and the tree of one merge base.
	maxTreeEntries = 50_000
	// maxPolicyBytes bounds the policy file at a merge base.
	maxPolicyBytes = 1 << 20
	// digestPolicyPath is the policy file, read at the merge base and never at the head (TECH_SPEC invariant 5).
	digestPolicyPath = "heimdall.yaml"
	// defaultGitHubAPI is the API root when GITHUB_API_URL is empty.
	defaultGitHubAPI = "https://api.github.com"
)

// digestDeps are the parts of the world the digest step reaches. Tests replace each one; production uses
// realDigestDeps.
type digestDeps struct {
	getenv func(string) string
	http   *http.Client
	wait   func(ctx context.Context, d time.Duration) error
	// now reads the clock once, at the start, so the period of a run is one instant.
	now func() time.Time
	// state reads every change of the state ref. Production uses readStateRef; a test replaces it, so the flag and the
	// report are tested without a GitHub call.
	state func(ctx context.Context, src digestSource, stderr io.Writer) (digest.Input, error)
}

func realDigestDeps() digestDeps {
	d := digestDeps{getenv: config.Getenv, http: &http.Client{Timeout: githubTimeout}, wait: clock.Wait, now: time.Now}
	d.state = func(ctx context.Context, src digestSource, stderr io.Writer) (digest.Input, error) {
		return readStateRef(ctx, src, stderr, d)
	}
	return d
}

// digestSource is where a digest reads its records.
type digestSource struct {
	repo   change.Repo
	ref    string
	window digest.Input
}

// digestOptions are the flags of `heimdall action digest`.
type digestOptions struct {
	days   int
	format string
	in     string
	repo   string
	ref    string
}

// runActionDigest is `heimdall action digest`: the weekly shadow digest (A4) and the weekly misses and latency report
// (A12) for one repository. It reads a local JSON snapshot with -in, or the state ref with -repo and the environment's
// token and MAC key. It writes the report to stdout and nothing anywhere else.
func runActionDigest(ctx context.Context, args []string, stdout, stderr io.Writer, deps digestDeps) int {
	options, err := parseDigestOptions(args, stderr)
	if err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			writeDiagnostic(stderr, fmt.Sprintf("heimdall action digest: %v\n", err))
		}
		return exitActionUsage
	}
	in, err := readDigestInput(ctx, options, stderr, deps)
	if err != nil {
		return actionFail(stderr, "digest", err)
	}
	if err := writeDigest(stdout, digest.Compute(in), options.format); err != nil {
		return actionFail(stderr, "digest", err)
	}
	return exitActionOK
}

// parseDigestOptions reads the flags. It refuses a flag it does not know, an argument it does not expect, and two
// sources at once, so a mistyped period never silently reports a different one.
func parseDigestOptions(args []string, stderr io.Writer) (digestOptions, error) {
	fs := flag.NewFlagSet("heimdall action digest", flag.ContinueOnError)
	fs.SetOutput(stderr)
	days := fs.Int("days", 7, "the length of the period in days, ending now; 0 is all recorded results")
	format := fs.String("format", "md", "the output format: md or json")
	in := fs.String("in", "", "read this JSON snapshot ({\"repo\", \"from\", \"to\", \"records\": [...]}) instead of "+
		"GitHub")
	repo := fs.String("repo", "", "read the state ref of this repository, as owner/name")
	ref := fs.String("ref", gitlog.DefaultRef, "the full ref that holds the state log")
	if err := fs.Parse(args); err != nil {
		return digestOptions{}, err
	}
	if fs.NArg() > 0 {
		return digestOptions{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if err := checkDigestOptions(*days, *format, *in, *repo); err != nil {
		return digestOptions{}, err
	}
	return digestOptions{days: *days, format: *format, in: *in, repo: *repo, ref: *ref}, nil
}

// checkDigestOptions checks the flag values that have a small set of right answers.
func checkDigestOptions(days int, format, in, repo string) error {
	if days < 0 {
		return fmt.Errorf("-days %d must be 0 or more", days)
	}
	if format != "md" && format != "json" {
		return fmt.Errorf("-format %q is neither md nor json", clipText(format, 20))
	}
	if in != "" && repo != "" {
		return errors.New("-in and -repo name two sources, so give one")
	}
	return nil
}

// readDigestInput reads the records from the source the flags name, and says plainly when none is configured. The
// window is applied by package digest, so a record outside it is counted apart rather than dropped in silence.
func readDigestInput(ctx context.Context, options digestOptions, stderr io.Writer, deps digestDeps) (digest.Input, error) {
	now := deps.now()
	window := digest.Input{To: now}
	if options.days > 0 {
		window.From = now.AddDate(0, 0, -options.days)
	}
	switch {
	case options.in != "":
		return readSnapshot(options.in, window)
	case options.repo != "":
		repo, err := change.ParseRepo(options.repo)
		if err != nil {
			return digest.Input{}, fmt.Errorf("-repo: %w", err)
		}
		window.Repo = repo.String()
		return deps.state(ctx, digestSource{repo: repo, ref: options.ref, window: window}, stderr)
	}
	return digest.Input{}, errors.New("no source: give -in <snapshot.json> for a local snapshot, or -repo owner/name " +
		"with GITHUB_TOKEN and HEIMDALL_LOG_KEY to read the state ref")
}

// readSnapshot reads a JSON snapshot of pull request records. The snapshot is a digest.Input: a repo, a window and the
// records. A window in the file is kept, because a snapshot of a past week is what a weekly report is about; the flags
// only fill a window the file leaves open.
func readSnapshot(path string, window digest.Input) (digest.Input, error) {
	raw, err := readBoundedFile(path, maxSnapshotBytes)
	if err != nil {
		return digest.Input{}, err
	}
	if err := checkNotAReport(raw); err != nil {
		return digest.Input{}, fmt.Errorf("%s: %w", path, err)
	}
	var in digest.Input
	if err := json.Unmarshal(raw, &in); err != nil {
		return digest.Input{}, fmt.Errorf("%s: %w (times are RFC 3339, for example 2026-11-17T12:00:00Z)", path, err)
	}
	if in.From.IsZero() {
		in.From = window.From
	}
	if in.To.IsZero() {
		in.To = window.To
	}
	if in.Repo == "" {
		in.Repo = window.Repo
	}
	return in, nil
}

// checkNotAReport refuses a document that is a rendered report rather than a snapshot. A report names its count "prs"
// and its records nowhere, so reading one back would give an empty period that reads as a clean week.
func checkNotAReport(raw []byte) error {
	var shape struct {
		PRs     json.RawMessage `json:"prs"`
		Records json.RawMessage `json:"records"`
	}
	if err := json.Unmarshal(raw, &shape); err != nil {
		return err
	}
	if len(shape.PRs) > 0 && shape.PRs[0] != '[' {
		return errors.New("this is a report, not a snapshot: a snapshot holds \"records\", so give the input a " +
			"generated snapshot or write one by hand")
	}
	if len(shape.PRs) == 0 && len(shape.Records) == 0 {
		return errors.New("no records: a snapshot holds a \"records\" array of pull request records")
	}
	return nil
}

// readBoundedFile reads a file of at most limit bytes. A larger file is refused rather than read in part, because a
// partly read snapshot would report numbers over half its records.
func readBoundedFile(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path) // #nosec G304 -- the path is the operator's own -in flag
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("%s is larger than %d bytes", path, limit)
	}
	return raw, nil
}

// writeDigest writes the report in the requested format.
func writeDigest(w io.Writer, r digest.Report, format string) error {
	if format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(r); err != nil {
			return fmt.Errorf("write JSON: %w", err)
		}
		return nil
	}
	if _, err := io.WriteString(w, digest.RenderMarkdown(r)); err != nil {
		return fmt.Errorf("write the report: %w", err)
	}
	return nil
}

// readStateRef reads every change log of the state ref and turns each change into one pull request record. The store
// checks the MAC and the chain of every log, so a log that fails verification is reported and not read on.
func readStateRef(ctx context.Context, src digestSource, stderr io.Writer, deps digestDeps) (digest.Input, error) {
	api, store, err := digestGitHub(src, deps)
	if err != nil {
		return digest.Input{}, err
	}
	ids, err := stateChanges(ctx, api, src)
	if err != nil {
		return digest.Input{}, err
	}
	if len(ids) > maxDigestChanges {
		writeDiagnostic(stderr, fmt.Sprintf("heimdall action digest: %d changes in %s, reading the newest %d\n",
			len(ids), src.ref, maxDigestChanges))
		ids = ids[:maxDigestChanges]
	}
	modes := &policyModes{api: api, repo: src.repo, stderr: stderr, seen: map[id.SHA]string{}}
	in := src.window
	for _, c := range ids {
		events, err := store.Load(ctx, c)
		if err != nil {
			writeDiagnostic(stderr, fmt.Sprintf("heimdall action digest: %s was not read: %v\n", c, err))
			continue
		}
		rec, ok := recordOf(ctx, events, modes.at)
		if !ok {
			continue
		}
		in.PRs = append(in.PRs, rec)
	}
	return in, nil
}

// digestGitHub builds the GitHub operations and the store the digest reads through. The token and the key are the
// caller's environment, read once here.
func digestGitHub(src digestSource, deps digestDeps) (*github.ActionAPI, *gitlog.Store, error) {
	tokens, err := github.NewStaticToken(strings.TrimSpace(deps.getenv("GITHUB_TOKEN")))
	if err != nil {
		return nil, nil, fmt.Errorf("GITHUB_TOKEN: %w", err)
	}
	api, err := github.NewAction(github.ActionConfig{BaseURL: apiURLOf(deps.getenv), Tokens: tokens,
		HTTPClient: deps.http, Clock: clock.System{}, Wait: deps.wait})
	if err != nil {
		return nil, nil, fmt.Errorf("github: %w", err)
	}
	key, err := logKeyOf(deps.getenv)
	if err != nil {
		return nil, nil, err
	}
	store, err := gitlog.New(gitlog.Config{Repo: src.repo, Git: api, Key: key, Ref: src.ref, Wait: deps.wait})
	if err != nil {
		return nil, nil, err
	}
	return api, store, nil
}

// stateChanges lists the change ids the state ref holds, newest pull request first. A tree GitHub marks truncated is
// refused rather than read in part.
func stateChanges(ctx context.Context, api *github.ActionAPI, src digestSource) ([]id.ChangeID, error) {
	tip, err := api.GetRef(ctx, src.repo, src.ref)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", src.ref, err)
	}
	commit, err := api.GetCommit(ctx, src.repo, tip)
	if err != nil {
		return nil, fmt.Errorf("read the commit of %s: %w", src.ref, err)
	}
	items, err := api.GetTree(ctx, src.repo, commit.Tree, maxTreeEntries)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", src.ref, err)
	}
	return changeIDsIn(items), nil
}

// changeIDsIn reads the change ids out of a state tree listing: one per changes/<change>.jsonl file, newest pull
// request first. Anything else in the tree is not a change log.
func changeIDsIn(items []ports.GitTreeItem) []id.ChangeID {
	var ids []id.ChangeID
	for _, it := range items {
		name, ok := strings.CutPrefix(it.Path, "changes/")
		if !ok || it.Type != "blob" {
			continue
		}
		name, ok = strings.CutSuffix(name, ".jsonl")
		if !ok {
			continue
		}
		c, err := id.ParseChangeID(name)
		if err != nil {
			continue
		}
		ids = append(ids, c)
	}
	sort.Slice(ids, func(i, j int) bool { return byNewestPR(ids[i], ids[j]) })
	return ids
}

// byNewestPR orders two change ids newest first. Two ticket streams sort after the pull requests, by id, so the order
// never depends on the order of the tree listing.
func byNewestPR(a, b id.ChangeID) bool {
	_, na, errA := a.PullRequest()
	_, nb, errB := b.PullRequest()
	switch {
	case errA == nil && errB == nil && na != nb:
		return na > nb
	case errA == nil && errB != nil:
		return true
	case errA != nil && errB == nil:
		return false
	}
	return a > b
}

// modeReader reads the action mode at a merge base. It is the seam the extraction takes, so a test reads a change's
// view without a GitHub call.
type modeReader func(ctx context.Context, base id.SHA) string

// recordOf turns one change's events into its pull request record. ok is false for a change that names no pull request:
// a ticket key on its own is not what a digest of pull requests counts.
func recordOf(ctx context.Context, events []change.Event, mode modeReader) (digest.PRRecord, bool) {
	view, err := change.Fold(events)
	if err != nil || view.PR == nil {
		return digest.PRRecord{}, false
	}
	rec := digest.PRRecord{Number: view.PR.Number, Head: string(view.PR.HeadSHA), At: lastEventAt(events),
		Mode: digest.ModeOf(mode(ctx, view.PR.Base))}
	if view.Action == nil {
		return rec, true // no Action fact: the pull request was never checked, and the digest says so
	}
	rec.Fast = fastOf(view.Action)
	rec.Proof = proofOf(view.Action)
	rec.ProofTimeMS = proofWallMS(events, view.Action)
	rec.CostMicros = view.Action.CostTotal().USDMicros
	rec.BreakGlassOpen, rec.BreakGlassReviewed = breakGlassOf(view.Action)
	return rec, true
}

// lastEventAt is when the change was last written, which is what places it in the period.
func lastEventAt(events []change.Event) time.Time {
	var at time.Time
	for _, e := range events {
		if e.OccurredAt.After(at) {
			at = e.OccurredAt
		}
	}
	return at
}

// fastOf is the last fast check of the change. Its wall time is not here: no event records when a fast check started,
// so a run from the state ref reports the fast wall time as not measured. A snapshot that measured it can carry it.
func fastOf(a *change.ActionInfo) digest.FastRecord {
	if a.Fast == nil {
		return digest.FastRecord{}
	}
	out := digest.FastRecord{Ran: true, Clauses: make([]digest.Clause, 0, len(a.Fast.Clauses))}
	for _, c := range a.Fast.Clauses {
		out.Clauses = append(out.Clauses, digest.Clause{ID: c.ID, Status: c.Status, Reason: c.Reason})
	}
	return out
}

// proofOf is the last finished proof run. A run that started and never reported is not a result, so it is Ran false.
func proofOf(a *change.ActionInfo) digest.ProofRecord {
	if a.Proof == nil || !a.Proof.Done {
		return digest.ProofRecord{}
	}
	p := a.Proof
	return digest.ProofRecord{Ran: true, Kind: p.Kind, Reason: p.Reason, Counts: digest.CountsOf(p.Counts.Counts())}
}

// proofWallMS is the wall time of the last finished proof run in milliseconds, from its start event to its report on
// the same head. A run with no start, or one whose report is not later, has no measurement and says so with 0.
func proofWallMS(events []change.Event, a *change.ActionInfo) int64 {
	if a == nil || a.Proof == nil || !a.Proof.Done {
		return 0
	}
	started, reported := proofEnds(events, a.Proof.WorkflowRun, a.Proof.Head)
	if started.IsZero() || !reported.After(started) {
		return 0
	}
	return reported.Sub(started).Milliseconds()
}

// proofEnds returns when one proof run started and when it reported on the same head. A run with no start event, or
// one whose report names another head, leaves one end zero.
func proofEnds(events []change.Event, run int64, head id.SHA) (started, reported time.Time) {
	for _, e := range events {
		p, err := change.Decode(e)
		if err != nil {
			continue
		}
		if !proofEndOf(p, run, head) {
			continue
		}
		if _, ok := p.(change.ProofStarted); ok {
			started = e.OccurredAt
			continue
		}
		reported = e.OccurredAt
	}
	return started, reported
}

// proofEndOf reports whether p is the start or the report of the run on head.
func proofEndOf(p change.Payload, run int64, head id.SHA) bool {
	switch p := p.(type) {
	case change.ProofStarted:
		return p.WorkflowRun == run && p.Head == head
	case change.ProofReported:
		return p.WorkflowRun == run && p.Head == head
	}
	return false
}

// breakGlassOf counts the bypasses of the change: those still waiting for a review, and those another approver reviewed.
func breakGlassOf(a *change.ActionInfo) (open, reviewed int) {
	for _, b := range a.BreakGlass {
		if b.Open() {
			open++
			continue
		}
		reviewed++
	}
	return open, reviewed
}

// policyModes reads the action mode at a merge base and remembers it, because many pull requests of one period share a
// base. A policy that cannot be read is reported once per base and reads as the default: shadow, which is the mode of an
// action area that names none. The mode is never taken from the head, because a change cannot edit the policy it is
// judged by (TECH_SPEC invariant 5).
type policyModes struct {
	api    *github.ActionAPI
	repo   change.Repo
	stderr io.Writer
	seen   map[id.SHA]string
}

// at returns the mode of the action area at base. An empty base has no policy to read, so the default stands.
func (m *policyModes) at(ctx context.Context, base id.SHA) string {
	if base == "" {
		return ""
	}
	if mode, ok := m.seen[base]; ok {
		return mode
	}
	mode, err := m.read(ctx, base)
	if err != nil {
		writeDiagnostic(m.stderr, fmt.Sprintf("heimdall action digest: the policy at %s was not read, so its pull "+
			"requests count as shadow: %v\n", base, err))
	}
	m.seen[base] = mode
	return mode
}

// read reads the policy at base and returns the mode of its action area.
func (m *policyModes) read(ctx context.Context, base id.SHA) (string, error) {
	commit, err := m.api.GetCommit(ctx, m.repo, base)
	if err != nil {
		return policy.Action{}.Effective().Mode, err
	}
	items, err := m.api.GetTree(ctx, m.repo, commit.Tree, maxTreeEntries)
	if err != nil {
		return policy.Action{}.Effective().Mode, err
	}
	for _, it := range items {
		if it.Path != digestPolicyPath || it.Type != "blob" {
			continue
		}
		raw, err := m.api.GetBlob(ctx, m.repo, it.SHA, maxPolicyBytes)
		if err != nil {
			return policy.Action{}.Effective().Mode, err
		}
		p, err := policyyaml.Decode(raw)
		if err != nil {
			return policy.Action{}.Effective().Mode, err
		}
		return p.Action.Effective().Mode, nil
	}
	return policy.Action{}.Effective().Mode, nil // no policy file: the defaults, which are shadow
}

// logKeyOf decodes HEIMDALL_LOG_KEY. It is the key the state ref is signed with, so a log read without it cannot be
// verified and is not read at all.
func logKeyOf(getenv func(string) string) ([]byte, error) {
	raw := strings.TrimSpace(getenv("HEIMDALL_LOG_KEY"))
	if raw == "" {
		return nil, errors.New("HEIMDALL_LOG_KEY is required to verify the state ref: give -in <snapshot.json> for a " +
			"snapshot instead")
	}
	key, err := hex.DecodeString(raw)
	if err != nil {
		return nil, errors.New("HEIMDALL_LOG_KEY must be hex")
	}
	if len(key) < gitlog.MinKeyBytes {
		return nil, fmt.Errorf("HEIMDALL_LOG_KEY must hold at least %d bytes", gitlog.MinKeyBytes)
	}
	return key, nil
}

// apiURLOf is GITHUB_API_URL, or the public API when the variable is empty.
func apiURLOf(getenv func(string) string) string {
	if v := strings.TrimSpace(getenv("GITHUB_API_URL")); v != "" {
		return v
	}
	return defaultGitHubAPI
}
