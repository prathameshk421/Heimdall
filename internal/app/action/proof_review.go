package action

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// The bounds of one tool result (AM12 follow-up: the loop does not cap a tool result, so each tool does). A result is
// what a model reads, so it stays small enough to keep a run inside its token cap.
const (
	maxReadBytes   = 2 << 20  // one file the read tool may open
	maxReadLines   = 300      // lines one read returns
	maxSearchHits  = 50       // matches one search returns
	maxSearchFile  = 1 << 20  // the largest file a search reads
	maxSearchLine  = 200      // bytes of one matching line
	maxPatchBytes  = 48 << 10 // the whole diff one list returns
	maxDiffFiles   = 40       // files one list names
	maxTestSource  = 60 << 10 // the largest repro test file a proposal may carry
	maxClaimRunes  = 600      // the domain's claim bound
	maxToolOutput  = 8 << 10  // the tail of one sandbox run
	maxResultTests = 30       // tests one sandbox result lists
	maxPromptClaim = 300      // bytes of one claim in the prompt
	maxDetailBytes = 300      // bytes of one proof detail in the artifact
)

// proposal is one finding the model asked to have proven, with the test it wrote. The test runs only in the sandbox.
type proposal struct {
	// Comment is the third-party claim this proposal answers, or 0 for a bug the review pass found.
	Comment  int64
	File     string
	First    int
	Last     int
	Severity finding.Severity
	Claim    string
	TestPath string
	TestName string
	Test     []byte
}

// reviewer is the model's view of one proof run: the changed files, the head checkout, the sandbox and its proposals.
// It exposes nothing the model could read back out: no environment, no key, no path outside the checkout.
type reviewer struct {
	run       *proofRun
	checkout  string
	proposals []proposal
	// claimed marks the third-party claims a proposal already answered, so one claim is proposed at most once.
	claimed map[int64]bool
}

// review runs the reviewer through the agent loop and keeps the proposals it accepted. A failed model call is an error
// the caller records as unknown: it is never silently a clean run.
func (r *proofRun) review(ctx context.Context) error {
	rv := &reviewer{run: r, checkout: r.in.Checkout, claimed: map[int64]bool{}}
	table := newToolTable(append(rv.tools(), proposeFinding{rv: rv}, finishReview{}))
	system, user := reviewerMessages(r.in)
	res, runErr := r.p.cfg.Loop.Run(ctx, ports.AgentRun{Model: r.p.cfg.ModelName, System: system, User: user,
		Tools: table.specs(), FinalTools: []string{finishReviewName}, Dispatcher: table,
		Budget: ports.AgentBudget{USD: r.act.Budget.PerPRUSD, InputTokens: int64(r.act.Budget.PerPRTokens.Input),
			OutputTokens: int64(r.act.Budget.PerPRTokens.Output)},
		MaxOutputTokens: r.act.Budget.CallOutputTokens, MaxTurns: maxProofTurns,
		Deadline: r.p.cfg.Clock.Now().Add(proofTimeout), RequireToolCall: true, Nudge: reviewerNudge})
	if res.Stop == "" {
		// The loop refused the request before it ran: a wiring fault, not an outcome of the run.
		return fmt.Errorf("action proof on %s: agent loop: %w", r.in.ChangeID, runErr)
	}
	r.res.Turns, r.res.Stop = len(res.Turns), res.Stop
	r.spent = proof.Spend{USD: res.CostUSD, InputTokens: int(res.Usage.InputTokens), OutputTokens: int(res.Usage.OutputTokens)}
	if runErr != nil {
		// The loop reports a cancellation, a model failure or a usage report that cannot be true as an error.
		r.res.StopDetail = runErr.Error()
	}
	r.proposals = rv.proposals
	// The review step ran whatever it found, so a run with no proposal is "not reproduced", not "no result".
	r.reviewed = true
	return nil
}

// reviewerNudge is the fixed message the agent loop sends after an answer with no tool call, at most twice a run.
const reviewerNudge = "Call a tool now. If you found no bug you can prove, call finish_review."

// reviewerPrompt is the system prompt. It states the tools to call and the proof rule, not how to reason.
const reviewerPrompt = `Answer only with tool calls. Propose each bug with propose_finding, which takes the Go test
that proves it. End with finish_review and a short summary, also when you found no bug you can prove.
You review a pull request. Find a bug that this change brings in, and prove it.
A finding counts only with proof: a Go test that fails on this PR head because of the bug. Use read_file, search and
list_changes to read the change. Use run_test to try a test before you propose it.
A claim from another review tool is a claim, not a fact: try to prove it, and if your test passes on this head, the
claim is not proven.`

// reviewerMessages builds the system prompt and the first user message: the change, its files, and the claims to try.
func reviewerMessages(in ProofIn) (system, user string) {
	var b strings.Builder
	fmt.Fprintf(&b, "Repository: %s. Pull request %d. The change touches %d files.", in.Repo, in.Number, len(in.Files))
	var paths []string
	for i, f := range in.Files {
		if i == maxPromptFiles {
			paths = append(paths, fmt.Sprintf("[%d more files not listed]", len(in.Files)-i))
			break
		}
		paths = append(paths, fmt.Sprintf("%s (%d+, %d-)", f.Path, f.Additions, f.Deletions))
	}
	if len(paths) > 0 {
		b.WriteString("\nChanged files:\n" + strings.Join(paths, "\n"))
	}
	if len(in.Claims) > 0 {
		b.WriteString("\n\nClaims from other review tools. Try to prove each one, or show it is not true on this head:\n")
		for _, c := range in.Claims {
			fmt.Fprintf(&b, "comment %d: %s at %s lines %d-%d (%s): %s\n", c.CommentID, c.Source, c.Location.File,
				c.Location.Lines[0], c.Location.Lines[1], c.Severity, clip(c.Claim, maxPromptClaim))
		}
	}
	return reviewerPrompt, b.String()
}

// clip cuts s to at most n bytes without splitting a rune, and says it cut.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}

// utf8RuneStart reports whether b starts a UTF-8 rune.
func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }

// reviewTool is one tool of the reviewer. Its returned string and its returned error both go back to the model; the tool
// bounds the string.
type reviewTool interface {
	Spec() ports.ModelTool
	Run(ctx context.Context, arguments json.RawMessage) (string, error)
}

// toolTable is the reviewer's ports.ToolDispatcher: it runs a tool call by name.
type toolTable struct {
	byName map[string]reviewTool
	list   []ports.ModelTool
}

var _ ports.ToolDispatcher = toolTable{}

// newToolTable indexes the tools. The loop refuses a duplicate or empty name in the specs.
func newToolTable(tools []reviewTool) toolTable {
	t := toolTable{byName: map[string]reviewTool{}}
	for _, tool := range tools {
		t.byName[tool.Spec().Name] = tool
		t.list = append(t.list, tool.Spec())
	}
	return t
}

// specs are the tool definitions the model sees.
func (t toolTable) specs() []ports.ModelTool { return t.list }

// Dispatch runs one tool call. The loop calls it only for a name in specs.
func (t toolTable) Dispatch(ctx context.Context, name string, args json.RawMessage) (string, error) {
	tool, ok := t.byName[name]
	if !ok {
		return "", fmt.Errorf("unknown tool %q", name)
	}
	return tool.Run(ctx, args)
}

// fnTool is a reviewTool made of a spec and a function.
type fnTool struct {
	spec ports.ModelTool
	run  func(ctx context.Context, args json.RawMessage) (string, error)
}

func (t fnTool) Spec() ports.ModelTool { return t.spec }

func (t fnTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	return t.run(ctx, args)
}

// tools returns the reviewer's tools. Each one bounds its own output, and none reads outside the checkout.
func (rv *reviewer) tools() []reviewTool {
	return []reviewTool{
		fnTool{spec: ports.ModelTool{Name: "list_changes",
			Description: "List the files the pull request changes, with their diffs.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)},
			run: rv.listChanges},
		fnTool{spec: ports.ModelTool{Name: "read_file",
			Description: "Read a file of the pull request head, with line numbers. At most 300 lines per call.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"start_line":{"type":"integer","minimum":1},"end_line":{"type":"integer","minimum":1}},"required":["path"],"additionalProperties":false}`)},
			run: rv.readFile},
		fnTool{spec: ports.ModelTool{Name: "search",
			Description: "Search the pull request head for a literal string. At most 50 matches.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":1},"path_prefix":{"type":"string"}},"required":["query"],"additionalProperties":false}`)},
			run: rv.search},
		fnTool{spec: ports.ModelTool{Name: "run_test",
			Description: "Write one new Go test file into the pull request head and run its package's tests in the sandbox. Returns each test's outcome and the output.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"test_path":{"type":"string"},"test_source":{"type":"string"}},"required":["test_path","test_source"],"additionalProperties":false}`)},
			run: rv.runTest},
	}
}

// listChanges writes the changed paths and their patches, cut at the size bound.
func (rv *reviewer) listChanges(_ context.Context, args json.RawMessage) (string, error) {
	if err := decodeArgs(args, &struct{}{}); err != nil {
		return "", err
	}
	files := rv.run.in.Files
	var b strings.Builder
	for i, f := range files {
		if i == maxDiffFiles {
			fmt.Fprintf(&b, "[%d more files not listed]\n", len(files)-i)
			break
		}
		fmt.Fprintf(&b, "%s (%s, %d+, %d-)\n", f.Path, f.Status, f.Additions, f.Deletions)
	}
	b.WriteString("\n")
	for i, f := range files {
		if i == maxDiffFiles {
			break
		}
		if f.Patch == "" {
			b.WriteString("--- " + f.Path + ": no diff (binary, or too large for the API)\n")
			continue
		}
		b.WriteString("--- " + f.Path + "\n" + f.Patch + "\n")
	}
	return clip(b.String(), maxPatchBytes), nil
}

// readFile reads one file of the head checkout, with line numbers, bounded to maxReadLines.
func (rv *reviewer) readFile(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Path      string `json:"path"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
	}
	if err := decodeArgs(args, &in); err != nil {
		return "", err
	}
	b, err := rv.headFile(in.Path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	start := max(in.StartLine, 1)
	end := in.EndLine
	if end == 0 || end > len(lines) {
		end = len(lines)
	}
	if start > end {
		return "", fmt.Errorf("start_line %d is after the last line %d", start, end)
	}
	cut := ""
	if end-start+1 > maxReadLines {
		end = start + maxReadLines - 1
		cut = fmt.Sprintf("[stopped at line %d of %d; ask for the next lines]\n", end, len(lines))
	}
	var out strings.Builder
	fmt.Fprintf(&out, "%s lines %d-%d of %d\n", in.Path, start, end, len(lines))
	for i := start; i <= end; i++ {
		fmt.Fprintf(&out, "%d\t%s\n", i, lines[i-1])
	}
	out.WriteString(cut)
	return out.String(), nil
}

// headFile reads one file of the checkout. A path that is not a clean relative path inside the tree is refused, and so
// is git's own directory: the tools never leave the checkout, and never read its history.
func (rv *reviewer) headFile(p string) ([]byte, error) {
	if err := repoPath(p); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(rv.checkout)
	if err != nil {
		return nil, fmt.Errorf("open the head tree: %w", err)
	}
	defer func() { _ = root.Close() }()
	info, err := root.Stat(filepath.FromSlash(p))
	if err != nil {
		return nil, fmt.Errorf("%s: no such file at the head", p)
	}
	if !info.Mode().IsRegular() || info.Size() > maxReadBytes {
		return nil, fmt.Errorf("%s: not a regular file of at most 2 MiB", p)
	}
	b, err := root.ReadFile(filepath.FromSlash(p))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	return b, nil
}

// repoPath refuses a path that is not a clean relative path inside the checkout, or that names git's own files.
func repoPath(p string) error {
	if p == "" || path.Clean(p) != p || !filepath.IsLocal(filepath.FromSlash(p)) || p == ".git" || strings.HasPrefix(p, ".git/") {
		return fmt.Errorf("path %q: want a clean relative path inside the repository", p)
	}
	return nil
}

// search walks the checkout for a literal string and stops at the match bound.
func (rv *reviewer) search(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Query      string `json:"query"`
		PathPrefix string `json:"path_prefix"`
	}
	if err := decodeArgs(args, &in); err != nil {
		return "", err
	}
	if in.Query == "" || strings.Contains(in.Query, "\n") {
		return "", errors.New("query: want one non-empty line")
	}
	prefix := strings.TrimSuffix(in.PathPrefix, "/")
	if prefix != "" && (path.Clean(prefix) != prefix || !filepath.IsLocal(filepath.FromSlash(prefix))) {
		return "", fmt.Errorf("path_prefix %q: want a clean relative path", in.PathPrefix)
	}
	root, err := os.OpenRoot(rv.checkout)
	if err != nil {
		return "", fmt.Errorf("open the head tree: %w", err)
	}
	defer func() { _ = root.Close() }()
	s := &searcher{root: root, ctx: ctx, query: in.Query, prefix: in.PathPrefix}
	walkErr := fs.WalkDir(root.FS(), ".", s.visit)
	if walkErr != nil && !errors.Is(walkErr, errSearchFull) {
		return "", fmt.Errorf("search: %w", walkErr)
	}
	return s.report(), nil
}

// errSearchFull ends the walk once the match bound is reached.
var errSearchFull = errors.New("full")

// searcher holds the state of one search. It skips .git and vendor, files over the bound and binary files, and stops
// at maxSearchHits.
type searcher struct {
	root   *os.Root
	ctx    context.Context
	query  string
	prefix string
	hits   []string
}

// visit is fs.WalkDirFunc. An entry the walk cannot read is skipped, so one permission problem does not end the search.
func (s *searcher) visit(p string, d fs.DirEntry, _ error) error {
	if d == nil {
		return nil
	}
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if d.IsDir() {
		if p == ".git" || p == "vendor" {
			return fs.SkipDir
		}
		return nil
	}
	if !s.wanted(p, d) {
		return nil
	}
	b, ok := s.read(p)
	if !ok {
		return nil
	}
	return s.scan(p, b)
}

// wanted says whether the walk reads this file: a regular file under the prefix, of at most maxSearchFile bytes.
func (s *searcher) wanted(p string, d fs.DirEntry) bool {
	if !d.Type().IsRegular() || !strings.HasPrefix(p, s.prefix) {
		return false
	}
	info, err := d.Info()
	return err == nil && info.Size() <= maxSearchFile
}

// read returns a file's bytes, or false when it cannot be read or looks binary.
func (s *searcher) read(p string) ([]byte, bool) {
	b, err := s.root.ReadFile(p)
	if err != nil || bytes.IndexByte(b, 0) >= 0 {
		return nil, false
	}
	return b, true
}

// scan adds the lines of one file that hold the query, and stops the walk at the match bound.
func (s *searcher) scan(p string, b []byte) error {
	for i, line := range strings.Split(string(b), "\n") {
		if !strings.Contains(line, s.query) {
			continue
		}
		if len(line) > maxSearchLine {
			line = line[:maxSearchLine] + "..."
		}
		s.hits = append(s.hits, fmt.Sprintf("%s:%d: %s", p, i+1, strings.TrimSpace(line)))
		if len(s.hits) == maxSearchHits {
			return errSearchFull
		}
	}
	return nil
}

// report writes the hits, and says when the limit cut the list short.
func (s searcher) report() string {
	if len(s.hits) == 0 {
		return "no match"
	}
	out := strings.Join(s.hits, "\n")
	if len(s.hits) == maxSearchHits {
		out += "\n[stopped at 50 matches; narrow the query or the path_prefix]"
	}
	return out
}

// runTest writes one new test file into the head tree and runs its package in the sandbox. It is the model's way to try
// a test, and it is the same call the proof makes, so what the model sees is what the proof decides on.
func (rv *reviewer) runTest(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		TestPath   string `json:"test_path"`
		TestSource string `json:"test_source"`
	}
	if err := decodeArgs(args, &in); err != nil {
		return "", err
	}
	if err := rv.checkTestFile(in.TestPath, in.TestSource); err != nil {
		return "", err
	}
	res, err := rv.run.sandboxRun(ctx, rv.run.in.Head, in.TestPath, []byte(in.TestSource))
	if err != nil {
		return "", err
	}
	return describeRun(res, declaredTests(in.TestSource)), nil
}

// checkTestFile refuses a test path or source the sandbox should not take. The path must be a new Go test file, so a
// proof never replaces a test the change has.
func (rv *reviewer) checkTestFile(testPath, source string) error {
	if err := repoPath(testPath); err != nil {
		return err
	}
	if !strings.HasSuffix(testPath, "_test.go") {
		return fmt.Errorf("test_path %q: want a *_test.go file", testPath)
	}
	if _, err := rv.headFile(testPath); err == nil {
		return fmt.Errorf("test_path %q exists at the head: use a new file name", testPath)
	}
	if source == "" || len(source) > maxTestSource {
		return errors.New("test_source: want a Go file of at most 60 KiB")
	}
	return nil
}

// declaredTests lists the top-level test functions a Go test file declares. It reads the "func TestXxx(" shape and not
// the Go grammar, because server code names no language (archcheck C4). A name it misses costs one refusal the model
// can fix; a name it invents would let a proof run a test that does not exist, and Prove refuses that anyway.
func declaredTests(source string) []string {
	var out []string
	for _, line := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(trimmed, "func ")
		if !ok || !isTestFuncPrefix(rest) {
			continue
		}
		name, _, ok := strings.Cut(rest, "(")
		if !ok {
			continue
		}
		out = append(out, name)
	}
	return out
}

// isTestFuncPrefix reports whether s starts with a Go test function name: Test, Benchmark or Example, then a rune that
// is not a lowercase letter (Go's own rule for a test's name).
func isTestFuncPrefix(s string) bool {
	for _, prefix := range []string{"Test", "Benchmark", "Example"} {
		if rest, ok := strings.CutPrefix(s, prefix); ok {
			if rest == "" || rest[0] < 'a' || rest[0] > 'z' {
				return true
			}
		}
	}
	return false
}

// sandboxRun runs one package's tests at sha with the overlay test written in, for the model's run_test tool. The job
// names the head the run serves, so the runner supersedes it with the rest of the run.
func (r *proofRun) sandboxRun(ctx context.Context, sha id.SHA, testPath string, source []byte) (ports.SandboxResult, error) {
	return r.sandboxRunAt(ctx, sha, testPath, source, nil)
}

// describeRun writes a sandbox result for the model: the counts, the tests it just wrote first, then the output tail.
func describeRun(res ports.SandboxResult, focus []string) string {
	tests := slices.Clone(res.Tests)
	focused := map[string]bool{}
	for _, n := range focus {
		focused[n] = true
	}
	rank := map[ports.Outcome]int{ports.OutcomeError: 0, ports.OutcomeFailure: 1, ports.OutcomePassed: 2, ports.OutcomeSkipped: 3}
	slices.SortFunc(tests, func(a, b ports.TestResult) int {
		if da, db := focused[a.Name], focused[b.Name]; da != db {
			if da {
				return -1
			}
			return 1
		}
		if d := rank[a.Outcome] - rank[b.Outcome]; d != 0 {
			return d
		}
		return strings.Compare(a.Name, b.Name)
	})
	var b strings.Builder
	fmt.Fprintf(&b, "passed %d, failed %d, errored %d\n", res.Passed, res.Failed, res.Errored)
	for i, t := range tests {
		if i == maxResultTests {
			fmt.Fprintf(&b, "[%d more tests not listed]\n", len(tests)-i)
			break
		}
		fmt.Fprintf(&b, "%s: %s\n", t.Name, t.Outcome)
	}
	b.WriteString("output:\n")
	b.WriteString(boundTail(res.Output, maxToolOutput))
	return b.String()
}

// boundTail keeps the last n bytes of s, cut at a line start, and says when it cut.
func boundTail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := s[len(s)-n:]
	if i := strings.IndexByte(cut, '\n'); i >= 0 {
		cut = cut[i+1:]
	}
	return "[earlier output cut]\n" + cut
}

// proposeFinding is a tool: each call records one proposal, and the model may call it for several findings before it
// calls finish_review. It ends nothing, so a model that proposes one bug can still find a second.
type proposeFinding struct{ rv *reviewer }

// Spec describes the tool to the model.
func (p proposeFinding) Spec() ports.ModelTool {
	return ports.ModelTool{Name: "propose_finding",
		Description: "Propose one bug the change brings in, with a Go test that fails on this head because of the bug and passes once the bug is fixed. At most 10 findings.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"comment_id":{"type":"integer","description":"The comment id of the third-party claim this finding answers, or 0 for a bug you found"},"file":{"type":"string"},"start_line":{"type":"integer","minimum":1},"end_line":{"type":"integer","minimum":1},"severity":{"type":"string","enum":["low","medium","high","critical"]},"claim":{"type":"string","maxLength":600},"test_path":{"type":"string"},"test_name":{"type":"string"},"test_source":{"type":"string"}},"required":["file","start_line","end_line","severity","claim","test_path","test_name","test_source"],"additionalProperties":false}`)}
}

// Run records the proposal and says so. The finding is proven after the review ends, so the model can go on.
func (p proposeFinding) Run(_ context.Context, args json.RawMessage) (string, error) {
	if err := p.rv.addProposal(args); err != nil {
		return "", err
	}
	return fmt.Sprintf("recorded as candidate %d; it is proven after the review ends", len(p.rv.proposals)), nil
}

// addProposal decodes, checks and records one proposal.
func (rv *reviewer) addProposal(args json.RawMessage) error {
	var in struct {
		CommentID  int64  `json:"comment_id"`
		File       string `json:"file"`
		StartLine  int    `json:"start_line"`
		EndLine    int    `json:"end_line"`
		Severity   string `json:"severity"`
		Claim      string `json:"claim"`
		TestPath   string `json:"test_path"`
		TestName   string `json:"test_name"`
		TestSource string `json:"test_source"`
	}
	if err := decodeArgs(args, &in); err != nil {
		return err
	}
	if len(rv.proposals) >= maxProposals {
		return fmt.Errorf("you already proposed %d findings, the most this run takes; call finish_review", maxProposals)
	}
	if err := rv.checkClaim(in.CommentID); err != nil {
		return err
	}
	p, err := rv.checkProposal(in.File, in.StartLine, in.EndLine, in.Severity, in.Claim)
	if err != nil {
		return err
	}
	if err := rv.checkTestFile(in.TestPath, in.TestSource); err != nil {
		return err
	}
	if names := declaredTests(in.TestSource); !slices.Contains(names, in.TestName) {
		return fmt.Errorf("test_name %q: test_source declares no top-level test of that name (it declares %v)", in.TestName, names)
	}
	p.Comment, p.TestPath, p.TestName, p.Test = in.CommentID, in.TestPath, in.TestName, []byte(in.TestSource)
	rv.proposals = append(rv.proposals, p)
	return nil
}

// checkClaim refuses a comment id that names no claim of this run, or one that a proposal already answered. A proposal
// with no claim names 0, which is a bug the review pass found.
func (rv *reviewer) checkClaim(id int64) error {
	if id == 0 {
		return nil
	}
	for _, c := range rv.run.in.Claims {
		if c.CommentID != id {
			continue
		}
		if rv.claimed[id] {
			return fmt.Errorf("comment_id %d is already proposed", id)
		}
		rv.claimed[id] = true
		return nil
	}
	return fmt.Errorf("comment_id %d is not one of the claims", id)
}

// checkProposal checks the place, the severity and the claim against the head tree and the domain bounds.
func (rv *reviewer) checkProposal(file string, start, end int, severity, claim string) (proposal, error) {
	src, err := rv.headFile(file)
	if err != nil {
		return proposal{}, err
	}
	n := strings.Count(strings.TrimSuffix(string(src), "\n"), "\n") + 1
	if start < 1 || end < start || end > n {
		return proposal{}, fmt.Errorf("lines %d-%d: want 1 <= start_line <= end_line <= %d", start, end, n)
	}
	sev, err := finding.ParseSeverity(severity)
	if err != nil {
		return proposal{}, fmt.Errorf("severity: %w", err)
	}
	claim = strings.TrimSpace(claim)
	if n := len([]rune(claim)); n < finding.ClaimMinChars || n > maxClaimRunes {
		return proposal{}, fmt.Errorf("claim: want %d to %d characters", finding.ClaimMinChars, maxClaimRunes)
	}
	return proposal{File: file, First: start, Last: end, Severity: sev, Claim: claim}, nil
}

// decodeArgs decodes the model's arguments strictly: an unknown field is an error the model can fix.
func decodeArgs(raw json.RawMessage, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("arguments: %w", err)
	}
	return nil
}

// finishReviewName is the name of the final tool.
const finishReviewName = "finish_review"

// finishReview ends the run. The proof run's output is the proposals, so any summary is accepted.
type finishReview struct{}

// Spec describes the tool to the model.
func (finishReview) Spec() ports.ModelTool {
	return ports.ModelTool{Name: finishReviewName,
		Description: "End the review. Call it once, after you proposed every finding you can support with a test.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"summary":{"type":"string","description":"One or two sentences on what you checked."}},"additionalProperties":false}`)}
}

// Run accepts any summary. A summary the model wrote badly is not worth a refusal: the review is already over.
func (finishReview) Run(_ context.Context, _ json.RawMessage) (string, error) { return "", nil }
