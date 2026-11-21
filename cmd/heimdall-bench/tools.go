package main

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
	"strconv"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Tool limits. Each tool bounds its own output (agentloop doc.go).
const (
	maxReadLines     = 300
	maxReadBytes     = 2 << 20
	maxSearchHits    = 50
	maxSearchFile    = 1 << 20
	maxSearchLine    = 200
	maxPatchBytes    = 48 << 10
	maxTestSource    = 64 << 10
	maxClaimRunes    = 600
	maxFindings      = 5
	maxTestsListed   = 30
	maxSandboxTimout = 10 * time.Minute
)

// proposal is one finding the reviewer proposed, with its candidate test.
type proposal struct {
	File       string
	Lines      bench.Span
	Severity   finding.Severity
	Claim      string
	TestPath   string
	TestName   string
	TestSource []byte
}

// fnTool is one tool of the reviewer: a spec and a function.
type fnTool struct {
	spec ports.ModelTool
	run  func(ctx context.Context, args json.RawMessage) (string, error)
}

// finalTool is the name of the final-answer tool.
const finalTool = "finish_review"

// finishReview is the final-answer tool. The review's output is the findings it proposed, so any summary is accepted:
// a summary the model wrote badly is not worth a refusal, because the review is already over.
func finishReview() fnTool {
	return fnTool{
		spec: ports.ModelTool{Name: finalTool, Description: "End the review. Call it once, after you proposed every finding you can support with a test.",
			Parameters: json.RawMessage(`{"type":"object","properties":{"summary":{"type":"string","description":"One or two sentences on what you checked."}},"additionalProperties":false}`)},
		run: func(_ context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Summary string `json:"summary"`
			}
			_ = decodeArgs(args, &in)
			return "accepted", nil
		},
	}
}

// toolbox is the reviewer's ports.ToolDispatcher: it runs a tool call by name.
type toolbox struct {
	byName map[string]fnTool
	list   []ports.ModelTool
}

var _ ports.ToolDispatcher = toolbox{}

// newToolbox indexes the tools. Names are unique, and the loop refuses a duplicate in the specs.
func newToolbox(tools []fnTool) toolbox {
	tb := toolbox{byName: map[string]fnTool{}}
	for _, t := range tools {
		tb.byName[t.spec.Name] = t
		tb.list = append(tb.list, t.spec)
	}
	return tb
}

// specs are the tool definitions the model sees.
func (tb toolbox) specs() []ports.ModelTool { return tb.list }

// Dispatch runs one tool call. The loop calls it only for a name in specs.
func (tb toolbox) Dispatch(ctx context.Context, name string, args json.RawMessage) (string, error) {
	t, ok := tb.byName[name]
	if !ok {
		return "", fmt.Errorf("unknown tool %q", name)
	}
	return t.run(ctx, args)
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

// tools returns the reviewer's tools: the ones the product gives it (read_code, search_code, list_changes,
// run_test, propose_finding). They read the head worktree only, so the fix and later history are not visible. onCall
// is told the latency of each call; it may be nil.
func (rp *replay) tools(onCall func(name string, seconds float64)) []fnTool {
	specs := rp.toolSpecs()
	out := make([]fnTool, len(specs))
	for i, s := range specs {
		out[i] = timed(s, onCall)
	}
	return out
}

// toolSpecs is the reviewer's tool list, before the timing wrapper.
func (rp *replay) toolSpecs() []fnTool {
	return []fnTool{
		{spec: ports.ModelTool{Name: "list_changes", Description: "List the files the pull request changes, with the unified diff (3 lines of context) from its base to its head.",
			Parameters: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)}, run: rp.listChanges},
		{spec: ports.ModelTool{Name: "read_code", Description: "Read a file at the pull request head, with line numbers. At most 300 lines per call.",
			Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"start_line":{"type":"integer","minimum":1},"end_line":{"type":"integer","minimum":1}},"required":["path"],"additionalProperties":false}`)}, run: rp.readCode},
		{spec: ports.ModelTool{Name: "search_code", Description: "Search the files at the pull request head for a literal string. At most 50 matches.",
			Parameters: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":1},"path_prefix":{"type":"string"}},"required":["query"],"additionalProperties":false}`)}, run: rp.searchCode},
		{spec: ports.ModelTool{Name: "run_test", Description: "Write one new " + testKind(rp.c) + " into the pull request head and run it in the sandbox. Returns each test's outcome and the output.",
			Parameters: json.RawMessage(`{"type":"object","properties":{"test_path":{"type":"string","description":` + strconv.Quote(testPathHint(rp.c)) + `},"test_source":{"type":"string"}},"required":["test_path","test_source"],"additionalProperties":false}`)}, run: rp.runTest},
		{spec: ports.ModelTool{Name: "propose_finding", Description: "Propose one bug the change brings in, with a " + testKind(rp.c) + " that fails on this head because of the bug and passes once the bug is fixed. At most 5 findings.",
			Parameters: json.RawMessage(`{"type":"object","properties":{"file":{"type":"string"},"start_line":{"type":"integer","minimum":1},"end_line":{"type":"integer","minimum":1},"severity":{"type":"string","enum":["low","medium","high","critical"]},"claim":{"type":"string","maxLength":600},"test_path":{"type":"string"},"test_name":{"type":"string"},"test_source":{"type":"string"}},"required":["file","start_line","end_line","severity","claim","test_path","test_name","test_source"],"additionalProperties":false}`)}, run: rp.proposeFinding},
	}
}

// timed wraps one tool so the case's progress learns the latency of each call. It changes nothing the model sees.
// A nil onCall returns the tool as it is.
func timed(t fnTool, onCall func(name string, seconds float64)) fnTool {
	if onCall == nil {
		return t
	}
	inner := t.run
	name := t.spec.Name
	t.run = func(ctx context.Context, args json.RawMessage) (string, error) {
		start := time.Now()
		out, err := inner(ctx, args)
		onCall(name, time.Since(start).Seconds())
		return out, err
	}
	return t
}

func (rp *replay) listChanges(_ context.Context, args json.RawMessage) (string, error) {
	if err := decodeArgs(args, &struct{}{}); err != nil {
		return "", err
	}
	var b strings.Builder
	for _, f := range rp.changes {
		fmt.Fprintf(&b, "%s (%s, %d hunks)\n", f.Path, f.Status, len(f.Hunks))
	}
	b.WriteString("\n")
	patch := rp.patch
	if len(patch) > maxPatchBytes {
		patch = patch[:maxPatchBytes] + "\n[diff cut at 48 KiB; use read_code for the rest]\n"
	}
	b.WriteString(patch)
	return b.String(), nil
}

// headFile reads one file of the head worktree through os.Root, so a symlink cannot leave the tree.
func (rp *replay) headFile(p string) ([]byte, error) {
	if err := repoPath(p); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(rp.workdir)
	if err != nil {
		return nil, fmt.Errorf("open head tree: %w", err)
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

// repoPath refuses a path that is not a clean relative path inside the repository, or that names git's own files.
func repoPath(p string) error {
	if p == "" || path.Clean(p) != p || !filepath.IsLocal(filepath.FromSlash(p)) || p == ".git" || strings.HasPrefix(p, ".git/") {
		return fmt.Errorf("path %q: want a clean relative path inside the repository", p)
	}
	return nil
}

func (rp *replay) readCode(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Path      string `json:"path"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
	}
	if err := decodeArgs(args, &in); err != nil {
		return "", err
	}
	b, err := rp.headFile(in.Path)
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

func (rp *replay) searchCode(ctx context.Context, args json.RawMessage) (string, error) {
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
	root, err := os.OpenRoot(rp.workdir)
	if err != nil {
		return "", fmt.Errorf("open head tree: %w", err)
	}
	defer func() { _ = root.Close() }()
	s := &searcher{root: root, ctx: ctx, query: in.Query, prefix: in.PathPrefix}
	walkErr := fs.WalkDir(root.FS(), ".", s.visit)
	if walkErr != nil && !errors.Is(walkErr, errSearchFull) {
		return "", fmt.Errorf("search: %w", walkErr)
	}
	return s.report(), nil
}

// errSearchFull ends the walk once the match limit is reached.
var errSearchFull = errors.New("full")

// searcher holds the state of one search_code walk. It skips directories the reviewer never needs, files that are too
// large or binary, and stops at maxSearchHits matches.
type searcher struct {
	root   *os.Root
	ctx    context.Context
	query  string
	prefix string
	hits   []string
}

// visit is fs.WalkDirFunc. It ignores the walk error on purpose: an entry the walk cannot read is skipped, so one
// permission problem does not end the whole search. A nil entry is the same case, and it is never read.
func (s *searcher) visit(p string, d fs.DirEntry, _ error) error {
	if d == nil {
		return nil
	}
	if s.ctx.Err() != nil {
		return s.ctx.Err()
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

// read returns a file's bytes, or false when the file cannot be read or looks binary.
func (s *searcher) read(p string) ([]byte, bool) {
	b, err := s.root.ReadFile(p)
	if err != nil || bytes.IndexByte(b, 0) >= 0 {
		return nil, false
	}
	return b, true
}

// scan adds the lines of one file that hold the query, and stops the walk at the match limit.
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

// checkTestFile refuses a test path or source the sandbox should not take. The path must be a new test file of the
// case's runner (reviewTestPath), so a proof never replaces a test the change has.
func (rp *replay) checkTestFile(testPath, source string) error {
	if err := repoPath(testPath); err != nil {
		return err
	}
	if err := reviewTestPath(rp.c, testPath); err != nil {
		return err
	}
	if _, err := rp.headFile(testPath); err == nil {
		return fmt.Errorf("test_path %q exists at the head: use a new file name", testPath)
	}
	if source == "" || len(source) > maxTestSource {
		return errors.New("test_source: want a file of at most 64 KiB")
	}
	return nil
}

// sandboxRun runs the package of testPath at sha with the test file written in. The case's progress gets one start
// line and one end line per run, so a slow suite is visible while the run is still going.
func (rp *replay) sandboxRun(ctx context.Context, sha, testPath string, source []byte) (ports.SandboxResult, error) {
	job := ports.SandboxJob{Kind: ports.SandboxRepro, Repo: change.Repo(rp.c.RepoSlug()), SHA: id.SHA(sha),
		Dir: path.Dir(testPath), Timeout: min(rp.c.Timeout, maxSandboxTimout)}
	label := treeLabel(sha)
	if rp.cp != nil {
		rp.cp.testStart(label)
	}
	start := time.Now()
	res, err := rp.sandbox.RunOverlay(ctx, job, []ports.File{{Path: testPath, Content: source}})
	if rp.cp != nil {
		rp.cp.testEnd(label, res, err, time.Since(start).Seconds())
	}
	return res, err
}

func (rp *replay) runTest(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		TestPath   string `json:"test_path"`
		TestSource string `json:"test_source"`
	}
	if err := decodeArgs(args, &in); err != nil {
		return "", err
	}
	if err := rp.checkTestFile(in.TestPath, in.TestSource); err != nil {
		return "", err
	}
	res, err := rp.sandboxRun(ctx, rp.head, in.TestPath, []byte(in.TestSource))
	if err != nil {
		return "", err
	}
	names, err := rp.focusNames(in.TestPath, in.TestSource, res)
	if err != nil {
		return "", err
	}
	return describeRun(res, names), nil
}

// describeRun writes a sandbox result for the model: counts, failing and errored tests first, then the output tail.
// focus names the tests the reviewer just wrote; they are listed before the rest, so a package with more tests than
// the listing takes still shows the answer the reviewer waits on.
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
		if i == maxTestsListed {
			fmt.Fprintf(&b, "[%d more tests not listed]\n", len(tests)-i)
			break
		}
		fmt.Fprintf(&b, "%s: %s\n", t.Name, t.Outcome)
	}
	b.WriteString("output:\n")
	b.WriteString(res.Output)
	return b.String()
}

func (rp *replay) proposeFinding(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
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
		return "", err
	}
	if len(rp.proposals) >= maxFindings {
		return "", fmt.Errorf("you already proposed %d findings, the most this review takes; call finish_review", maxFindings)
	}
	p, err := rp.checkProposal(in.File, in.StartLine, in.EndLine, in.Severity, in.Claim)
	if err != nil {
		return "", err
	}
	if err := rp.checkTestFile(in.TestPath, in.TestSource); err != nil {
		return "", err
	}
	name, err := reviewTestName(rp.c, in.TestPath, in.TestSource, in.TestName)
	if err != nil {
		return "", err
	}
	p.TestPath, p.TestName, p.TestSource = in.TestPath, name, []byte(in.TestSource)
	rp.proposals = append(rp.proposals, p)
	return fmt.Sprintf("recorded as F%d; it is proven after the review ends", len(rp.proposals)), nil
}

func (rp *replay) checkProposal(file string, start, end int, severity, claim string) (proposal, error) {
	src, err := rp.headFile(file)
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
	if claim == "" || len([]rune(claim)) > maxClaimRunes {
		return proposal{}, errors.New("claim: want one to 600 characters")
	}
	return proposal{File: file, Lines: bench.Span{Start: start, End: end}, Severity: sev, Claim: claim}, nil
}

func allLines(src string) []int {
	n := strings.Count(src, "\n") + 1
	out := make([]int, n)
	for i := range out {
		out[i] = i + 1
	}
	return out
}
