package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/agentloop"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// The agent loop limits per case: ACTION_MODE section 7 (4k output per answer, 800k input and 40k output per PR) and
// the 15-minute proof run of 5.2.
const (
	maxOutputPerAnswer = 4096
	inputTokensPerCase = 800_000
	outputTokensPerCas = 40_000
	maxTurnsPerCase    = 40
	caseDeadline       = 15 * time.Minute
)

// testingCapUSD is the repo's testing cap (CLAUDE.md), the default of -max-cost.
const testingCapUSD = 3.0

// modelPrices returns the OpenRouter prices in USD per million tokens, read on 2026-11-21. The bench bills the model
// the brief names, so a live run needs no price flags; a flag still wins, because a provider can change a price.
func modelPrices() map[string]bench.Price {
	return map[string]bench.Price{
		"deepseek/deepseek-v4-flash": {InputPerM: 0.09, CachedInputPerM: 0.018, OutputPerM: 0.18},
	}
}

// priceFor picks the price of the run: the flags when they are set, the table when they are not. A model the table
// does not know needs both price flags, because a run with no price would be blind to its spend.
func priceFor(model string, flags bench.Price) (bench.Price, error) {
	set := flags.InputPerM != 0 || flags.OutputPerM != 0
	if set {
		if !(flags.InputPerM > 0) || !(flags.OutputPerM > 0) {
			return bench.Price{}, errors.New("review: -price-input and -price-output must both be positive USD per million tokens")
		}
		if flags.CachedInputPerM < 0 {
			flags.CachedInputPerM = flags.InputPerM
		}
		return flags, nil
	}
	p, ok := modelPrices()[model]
	if !ok {
		return bench.Price{}, fmt.Errorf("review: no price for %s: pass -price-input and -price-output from the provider's page", model)
	}
	return p, nil
}

// parseCaseIDs splits the -only flag: commas or spaces, with a repeated or empty id refused as a typo.
func parseCaseIDs(s string) ([]string, error) {
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })
	seen := map[string]bool{}
	var out []string
	for _, id := range fields {
		if seen[id] {
			return nil, fmt.Errorf("-only names %q twice", id)
		}
		seen[id] = true
		out = append(out, id)
	}
	if strings.TrimSpace(s) != "" && len(out) == 0 {
		return nil, errors.New("-only: want one case id or a comma-separated list")
	}
	return out, nil
}

// reviewOptions are the flags of the review command.
type reviewOptions struct {
	manifest   string
	cache      string
	out        string
	md         string
	only       string
	model      string
	keyFile    string
	baseURL    string
	price      bench.Price
	maxCost    float64
	perCase    float64
	parallel   int
	blockAt    finding.Severity
	commandFor string // the command line written into the report
}

// MaxParallel is the most cases a review runs at once.
const maxParallel = 8

func parseReviewFlags(args []string, stderr io.Writer) (reviewOptions, error) {
	fs := flag.NewFlagSet("review", flag.ContinueOnError)
	fs.SetOutput(stderr)
	common := addCommon(fs)
	o := reviewOptions{}
	fs.StringVar(&o.out, "out", "", "write the JSON results to this file (required)")
	fs.StringVar(&o.md, "md", "", "append the Markdown results to this file, such as docs/sota/BENCH.md")
	fs.StringVar(&o.only, "only", "", "comma-separated valid case ids to review; default every valid Go case")
	fs.StringVar(&o.model, "model", "", "the model id, such as deepseek/deepseek-v4-flash (required)")
	fs.StringVar(&o.keyFile, "key-file", "", "the file that holds the model key, read through internal/platform/config (required)")
	fs.StringVar(&o.baseURL, "base-url", "https://openrouter.ai/api/v1", "the OpenAI-compatible API root")
	fs.Float64Var(&o.price.InputPerM, "price-input", 0, "USD per million input tokens; default the table's price for -model")
	fs.Float64Var(&o.price.CachedInputPerM, "price-cached", -1, "USD per million cache-read tokens; unset means the input price")
	fs.Float64Var(&o.price.OutputPerM, "price-output", 0, "USD per million output tokens; default the table's price for -model")
	fs.Float64Var(&o.maxCost, "max-cost", testingCapUSD, "hard cap in USD for the whole run; no call that could pass it is made")
	fs.Float64Var(&o.perCase, "per-case-cost", 0.50, "the agent loop budget in USD for one case (ACTION_MODE 7)")
	fs.IntVar(&o.parallel, "parallel", 1, "how many cases to review at once, at most 8; the cost cap stays shared")
	block := fs.String("block-severity", "medium", "a proven finding at or above this severity blocks")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	o.manifest, o.cache = common.manifest, common.cache
	if o.out == "" {
		return o, errors.New("review: -out is required")
	}
	if strings.TrimSpace(o.model) == "" {
		return o, errors.New("review: -model is required")
	}
	if o.keyFile == "" {
		return o, errors.New("review: -key-file is required")
	}
	if o.parallel < 1 || o.parallel > maxParallel {
		return o, fmt.Errorf("review: -parallel %d: want 1 to %d", o.parallel, maxParallel)
	}
	p, err := priceFor(o.model, o.price)
	if err != nil {
		return o, err
	}
	o.price = p
	if _, err := parseCaseIDs(o.only); err != nil {
		return o, fmt.Errorf("review: -only: %w", err)
	}
	sev, err := finding.ParseSeverity(*block)
	if err != nil {
		return o, fmt.Errorf("review: -block-severity: %w", err)
	}
	o.blockAt = sev
	o.commandFor = "heimdall-bench review " + strings.Join(args, " ")
	return o, nil
}

func runReview(ctx context.Context, args []string, stderr io.Writer, r runner) (int, error) {
	o, err := parseReviewFlags(args, stderr)
	if err != nil {
		return exitUsage, err
	}
	model, err := liveModel(o)
	if err != nil {
		return exitUsage, err
	}
	return reviewRun(ctx, o, r, model, stderr)
}

// reviewReport is the results file of a review run.
type reviewReport struct {
	HeimdallCommit string          `json:"heimdall_commit"`
	Started        string          `json:"started"`
	Machine        string          `json:"machine"`
	GoVersion      string          `json:"go_version"`
	Manifest       string          `json:"manifest"`
	Command        string          `json:"command"`
	Model          string          `json:"model"`
	Price          bench.Price     `json:"price"`
	MaxCostUSD     float64         `json:"max_cost_usd"`
	PerCaseUSD     float64         `json:"per_case_usd"`
	BlockSeverity  string          `json:"block_severity"`
	Sandbox        string          `json:"sandbox"`
	Seconds        float64         `json:"seconds"`
	Stopped        string          `json:"stopped,omitempty"`
	Summary        bench.Summary   `json:"summary"`
	Turns          bench.TurnStats `json:"turns"`
	Cases          []reviewCase    `json:"cases"`
}

// reviewCase is one case in the results file.
type reviewCase struct {
	bench.CaseScore
	Head     string                `json:"head"`
	Base     string                `json:"base"`
	Fixed    string                `json:"fixed"`
	Stop     agentloop.StopReason  `json:"stop,omitempty"`
	Error    string                `json:"error,omitempty"`
	Truth    *bench.TruthMap       `json:"truth_on_head,omitempty"`
	Findings []bench.ScoredFinding `json:"findings"`
}

// reviewRun reviews every selected valid case with model behind the hard cost cap, then writes the results. The exit
// status is 0 when every case was scored, and 1 when one was not (an error, or the cost cap stopped the run).
//
// With -parallel N the cases are reviewed N at a time. One cost cap is shared by every worker, so the run's spend never
// passes -max-cost. Every case keeps its own repository cache, sandbox and worktrees, and the results file keeps the
// manifest order whatever order the cases end in.
func reviewRun(ctx context.Context, o reviewOptions, r runner, model ports.ModelChat, stderr io.Writer) (int, error) {
	cases, err := reviewCases(o)
	if err != nil {
		return exitUsage, err
	}
	costCap, err := bench.NewCostCap(o.maxCost)
	if err != nil {
		return exitUsage, err
	}
	start := time.Now()
	base := newReport(ctx, r, o.manifest, start)
	rep := reviewReport{HeimdallCommit: base.HeimdallCommit, Started: base.Started, Machine: base.Machine,
		GoVersion: base.GoVersion, Manifest: o.manifest, Command: o.commandFor, Model: o.model, Price: o.price,
		MaxCostUSD: o.maxCost, PerCaseUSD: o.perCase, BlockSeverity: string(o.blockAt), Sandbox: sandboxNote}
	prog := newProgress(stderr)
	rep.Cases, rep.Stopped = runCases(ctx, o, r, model, costCap, prog, cases)
	rep.Seconds = time.Since(start).Seconds()
	scores := make([]bench.CaseScore, len(rep.Cases))
	for i, c := range rep.Cases {
		scores[i] = c.CaseScore
	}
	rep.Summary = bench.Summarize(scores)
	rep.Turns = bench.TurnSummary(scores)
	if err := writeReview(o, rep); err != nil {
		return exitUsage, err
	}
	if rep.Summary.Unfinished > 0 {
		return exitDisagree, nil
	}
	return exitAgrees, nil
}

// runCases reviews the cases with up to -parallel workers and returns one result per case, in the manifest order.
// stopped is "cost cap" when the cap ended the run.
func runCases(ctx context.Context, o reviewOptions, r runner, model ports.ModelChat, costCap *bench.CostCap,
	prog *progress, cases []bench.Case) (out []reviewCase, stopped string) {
	run := func(i int) (reviewCase, bool) {
		return reviewOne(ctx, o, r, model, costCap, prog, cases[i])
	}
	out, capped := runPool(o.parallel, len(cases), run)
	if !capped {
		return out, ""
	}
	for i, c := range out {
		if c.ID == "" {
			out[i] = reviewCase{CaseScore: bench.CaseScore{ID: cases[i].ID, Kind: cases[i].Kind}, Error: "not run: cost cap"}
		}
	}
	return out, "cost cap"
}

// runPool runs one case at a time in each of n workers, and returns the results in slot order whatever order the work
// ends in. capped is true when a worker reported that the cost cap ended the run; no case after that starts.
func runPool(n, count int, run func(i int) (reviewCase, bool)) (out []reviewCase, capped bool) {
	out = make([]reviewCase, count)
	workers := max(min(n, count), 1)
	var mu sync.Mutex
	next := 0
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				mu.Lock()
				if next >= count || capped {
					mu.Unlock()
					return
				}
				i := next
				next++
				mu.Unlock()
				rc, stop := run(i)
				mu.Lock()
				out[i] = rc
				capped = capped || stop
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return out, capped
}

// reviewCases picks the valid Go cases, or the -only ones, which must be valid Go cases too.
func reviewCases(o reviewOptions) ([]bench.Case, error) {
	m, err := loadManifest(o.manifest)
	if err != nil {
		return nil, err
	}
	ids, err := parseCaseIDs(o.only)
	if err != nil {
		return nil, err
	}
	out, err := pickCases(m.Cases, ids)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, errors.New("review: no valid Go case to review")
	}
	return out, nil
}

// pickCases keeps the valid Go cases, or, when ids is not empty, exactly those ids, which must all be valid Go cases.
func pickCases(cases []bench.Case, ids []string) ([]bench.Case, error) {
	var out []bench.Case
	have := map[string]bool{}
	for _, c := range cases {
		if len(ids) > 0 && !slices.Contains(ids, c.ID) {
			continue
		}
		if c.Status != bench.StatusValid || c.Language != "go" {
			if len(ids) > 0 {
				return nil, fmt.Errorf("review: %s is not a valid Go case", c.ID)
			}
			continue
		}
		out = append(out, c)
		have[c.ID] = true
	}
	for _, id := range ids {
		if !have[id] {
			return nil, fmt.Errorf("review: -only %q names no valid Go case in the manifest", id)
		}
	}
	return out, nil
}

// reviewOne replays one case, runs the reviewer and proves its findings. stop is true when the cost cap ended it. The
// case has its own repository cache, sandbox and progress lines, so several cases can run at once on one cost cap.
func reviewOne(ctx context.Context, o reviewOptions, r runner, model ports.ModelChat, costCap *bench.CostCap,
	prog *progress, c bench.Case) (rc reviewCase, stop bool) {
	start := time.Now()
	rc = reviewCase{CaseScore: bench.CaseScore{ID: c.ID, Kind: c.Kind}}
	defer func() { rc.Seconds = time.Since(start).Round(100 * time.Millisecond).Seconds() }()
	cp := newCaseProgress(prog, c.ID)
	cp.startCase(c.Kind)
	defer func() { cp.end(rc, costCap.Spent()) }()
	rep, budget, err := openCase(ctx, o, r, costCap, cp, c)
	if err != nil {
		rc.Error = err.Error()
		return rc, errors.Is(err, bench.ErrCostCap)
	}
	defer rep.close(ctx)
	rc.Head, rc.Base, rc.Fixed, rc.Truth = rep.head, rep.base, rep.fixed, rep.truth
	if stop, err := runReviewer(ctx, o, model, costCap, budget, cp, rep, &rc); err != nil {
		rc.Error = err.Error()
		return rc, stop
	}
	proveProposals(ctx, rep, &rc)
	rc.CaseScore = scoreCase(c, o.blockAt, rc)
	return rc, false
}

// openCase sets the case up: the per-case budget, the repository, the sandbox and the replayed trees.
func openCase(ctx context.Context, o reviewOptions, r runner, costCap *bench.CostCap, cp *caseProgress,
	c bench.Case) (*replay, float64, error) {
	budget, err := costCap.CaseBudget(o.perCase)
	if err != nil {
		return nil, 0, fmt.Errorf("not run: %w", err)
	}
	rp, err := openRepo(ctx, r, o.cache, c)
	if err != nil {
		return nil, 0, fmt.Errorf("open cache: %w", err)
	}
	sb := newLocalSandbox()
	sb.add(c.RepoSlug(), rp)
	rep, err := prepareReplay(ctx, rp, sb, c)
	if err != nil {
		return nil, 0, fmt.Errorf("replay: %w", err)
	}
	rep.cp = cp
	return rep, budget, nil
}

// runReviewer runs the agent loop on the case and fills the turn, the stop reason, the cost and the tokens of rc. The
// returned stop marks a run the cost cap ended.
func runReviewer(ctx context.Context, o reviewOptions, model ports.ModelChat, costCap *bench.CostCap, budget float64,
	cp *caseProgress, rep *replay, rc *reviewCase) (stop bool, err error) {
	capped := cappedModel{inner: model, cap: costCap, price: o.price, prog: cp}
	loop, err := agentloop.New(capped, clock.System{}, agentloop.Config{Model: o.model, MaxOutputTokens: maxOutputPerAnswer,
		BudgetUSD: budget, InputTokenCap: inputTokensPerCase, OutputTokenCap: outputTokensPerCas,
		Deadline: time.Now().Add(caseDeadline), MaxTurns: maxTurnsPerCase}, rep.tools(cp.toolCall), []agentloop.Submitter{finishReview{}})
	if err != nil {
		return false, fmt.Errorf("agent loop: %w", err)
	}
	res, runErr := loop.Run(ctx, reviewerMessages(rep.c, rep))
	rc.Stop, rc.Turns, rc.CostUSD = res.Reason, res.Turns, res.CostUSD
	rc.TurnSeconds, rc.ToolSeconds = cp.turnsSeconds(), cp.toolSeconds()
	rc.InputTokens, rc.CachedTokens, rc.OutputTokens = res.Usage.InputTokens, res.Usage.CachedInputTokens, res.Usage.OutputTokens
	if runErr != nil {
		return errors.Is(runErr, bench.ErrCostCap), errors.New(runErr.Error())
	}
	return false, nil
}

// proveProposals runs the proof rule on every proposal and adds its result to the case.
func proveProposals(ctx context.Context, rep *replay, rc *reviewCase) {
	for i, p := range rep.proposals {
		rc.Findings = append(rc.Findings, rep.prove(ctx, i+1, p))
	}
}

// scoreCase scores the findings of a finished case and copies the cost and the tokens into the score.
func scoreCase(c bench.Case, blockAt finding.Severity, rc reviewCase) bench.CaseScore {
	var truth []bench.FileTruth
	if rc.Truth != nil {
		truth = rc.Truth.Truth
	}
	score := bench.ScoreCase(c.Kind, truth, rc.Findings, blockAt)
	score.ID, score.Scored = c.ID, true
	score.CostUSD, score.InputTokens, score.CachedTokens, score.OutputTokens = rc.CostUSD, rc.InputTokens, rc.CachedTokens, rc.OutputTokens
	return score
}

// reviewerPrompt is the reviewer's system prompt. It states the task and the proof rule, not how to reason.
const reviewerPrompt = `You review a pull request in a Go repository. Find bugs that this change brings in.
A finding counts only with proof: a Go test that fails on this head because of the bug, in two separate runs, and
passes once the suspected lines are fixed. A test that does not build proves nothing. Use run_test to try a test
before you propose it. Propose each bug with propose_finding (at most 5), then call finish_review. If you find no
bug you can prove, propose nothing and call finish_review.`

func reviewerMessages(c bench.Case, rep *replay) []ports.ModelMessage {
	var files []string
	for _, f := range rep.changes {
		files = append(files, f.Path)
	}
	user := fmt.Sprintf("Repository: %s. The pull request changes %d files: %s. Use list_changes to read the diff.",
		c.RepoSlug(), len(files), strings.Join(files, ", "))
	return []ports.ModelMessage{{Role: ports.ModelRoleSystem, Content: reviewerPrompt}, {Role: ports.ModelRoleUser, Content: user}}
}

func writeReview(o reviewOptions, rep reviewReport) error {
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return fmt.Errorf("encode results: %w", err)
	}
	if err := os.WriteFile(o.out, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("write results: %w", err)
	}
	if o.md == "" {
		return nil
	}
	f, err := os.OpenFile(o.md, os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304 -- the operator names the file
	if err != nil {
		return fmt.Errorf("open markdown: %w", err)
	}
	if _, err := io.WriteString(f, reviewMarkdown(rep)); err != nil {
		_ = f.Close()
		return fmt.Errorf("append markdown: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close markdown: %w", err)
	}
	return nil
}
