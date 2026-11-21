package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
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
	blockAt    finding.Severity
	commandFor string // the command line written into the report
}

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
	fs.Float64Var(&o.price.InputPerM, "price-input", 0, "USD per million input tokens (required)")
	fs.Float64Var(&o.price.CachedInputPerM, "price-cached", -1, "USD per million cache-read tokens; default the input price")
	fs.Float64Var(&o.price.OutputPerM, "price-output", 0, "USD per million output tokens (required)")
	fs.Float64Var(&o.maxCost, "max-cost", testingCapUSD, "hard cap in USD for the whole run; no call that could pass it is made")
	fs.Float64Var(&o.perCase, "per-case-cost", 0.50, "the agent loop budget in USD for one case (ACTION_MODE 7)")
	block := fs.String("block-severity", "medium", "a proven finding at or above this severity blocks")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	o.manifest, o.cache = common.manifest, common.cache
	if o.out == "" {
		return o, errors.New("review: -out is required")
	}
	if o.price.CachedInputPerM < 0 {
		o.price.CachedInputPerM = o.price.InputPerM
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
	HeimdallCommit string        `json:"heimdall_commit"`
	Started        string        `json:"started"`
	Machine        string        `json:"machine"`
	GoVersion      string        `json:"go_version"`
	Manifest       string        `json:"manifest"`
	Command        string        `json:"command"`
	Model          string        `json:"model"`
	Price          bench.Price   `json:"price"`
	MaxCostUSD     float64       `json:"max_cost_usd"`
	PerCaseUSD     float64       `json:"per_case_usd"`
	BlockSeverity  string        `json:"block_severity"`
	Sandbox        string        `json:"sandbox"`
	Seconds        float64       `json:"seconds"`
	Stopped        string        `json:"stopped,omitempty"`
	Summary        bench.Summary `json:"summary"`
	Cases          []reviewCase  `json:"cases"`
}

// reviewCase is one case in the results file.
type reviewCase struct {
	bench.CaseScore
	Head     string                `json:"head"`
	Base     string                `json:"base"`
	Fixed    string                `json:"fixed"`
	Stop     agentloop.StopReason  `json:"stop,omitempty"`
	Turns    int                   `json:"turns"`
	Error    string                `json:"error,omitempty"`
	Truth    *bench.TruthMap       `json:"truth_on_head,omitempty"`
	Findings []bench.ScoredFinding `json:"findings"`
}

// reviewRun reviews every selected valid case with model behind the hard cost cap, then writes the results. The exit
// status is 0 when every case was scored, and 1 when one was not (an error, or the cost cap stopped the run).
func reviewRun(ctx context.Context, o reviewOptions, r runner, model ports.ModelChat, stderr io.Writer) (int, error) {
	cases, err := reviewCases(o)
	if err != nil {
		return exitUsage, err
	}
	costCap, err := bench.NewCostCap(o.maxCost)
	if err != nil {
		return exitUsage, err
	}
	capped := cappedModel{inner: model, cap: costCap, price: o.price}
	start := time.Now()
	base := newReport(ctx, r, o.manifest, start)
	rep := reviewReport{HeimdallCommit: base.HeimdallCommit, Started: base.Started, Machine: base.Machine,
		GoVersion: base.GoVersion, Manifest: o.manifest, Command: o.commandFor, Model: o.model, Price: o.price,
		MaxCostUSD: o.maxCost, PerCaseUSD: o.perCase, BlockSeverity: string(o.blockAt), Sandbox: sandboxNote}
	sb := newLocalSandbox()
	for _, c := range cases {
		if rep.Stopped != "" {
			rep.Cases = append(rep.Cases, reviewCase{CaseScore: bench.CaseScore{ID: c.ID, Kind: c.Kind}, Error: "not run: " + rep.Stopped})
			continue
		}
		rc, stop := reviewOne(ctx, o, r, sb, capped, costCap, c)
		rep.Cases = append(rep.Cases, rc)
		if stop {
			rep.Stopped = "cost cap"
		}
		writeDiagnostic(stderr, fmt.Sprintf("%-20s %-5s scored=%-5v proposed=%d proven=%d hit=%v fp=%v $%.4f %6.1fs %s\n",
			c.ID, c.Kind, rc.Scored, rc.Proposed, rc.Proven, rc.Hit, rc.FalsePositive, rc.CostUSD, rc.Seconds, rc.Error))
	}
	rep.Seconds = time.Since(start).Seconds()
	scores := make([]bench.CaseScore, len(rep.Cases))
	for i, c := range rep.Cases {
		scores[i] = c.CaseScore
	}
	rep.Summary = bench.Summarize(scores)
	if err := writeReview(o, rep); err != nil {
		return exitUsage, err
	}
	if rep.Summary.Unfinished > 0 {
		return exitDisagree, nil
	}
	return exitAgrees, nil
}

// reviewCases picks the valid Go cases, or the -only ones, which must be valid Go cases too.
func reviewCases(o reviewOptions) ([]bench.Case, error) {
	m, err := loadManifest(o.manifest)
	if err != nil {
		return nil, err
	}
	picked, err := selectCases(m, o.only)
	if err != nil {
		return nil, err
	}
	var out []bench.Case
	for _, c := range picked {
		switch {
		case c.Status == bench.StatusValid && c.Language == "go":
			out = append(out, c)
		case o.only != "":
			return nil, fmt.Errorf("review: %s is not a valid Go case", c.ID)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("review: no valid Go case to review")
	}
	return out, nil
}

// reviewOne replays one case, runs the reviewer and proves its findings. stop is true when the cost cap ended it.
func reviewOne(ctx context.Context, o reviewOptions, r runner, sb *localSandbox, model ports.ModelChat,
	costCap *bench.CostCap, c bench.Case) (rc reviewCase, stop bool) {
	start := time.Now()
	rc = reviewCase{CaseScore: bench.CaseScore{ID: c.ID, Kind: c.Kind}}
	defer func() { rc.Seconds = time.Since(start).Round(100 * time.Millisecond).Seconds() }()
	budget, err := costCap.CaseBudget(o.perCase)
	if err != nil {
		rc.Error = "not run: " + err.Error()
		return rc, errors.Is(err, bench.ErrCostCap)
	}
	rp, err := openRepo(ctx, r, o.cache, c)
	if err != nil {
		rc.Error = "open cache: " + err.Error()
		return rc, false
	}
	sb.add(c.RepoSlug(), rp)
	rep, err := prepareReplay(ctx, rp, sb, c)
	if err != nil {
		rc.Error = "replay: " + err.Error()
		return rc, false
	}
	defer rep.close(ctx)
	rc.Head, rc.Base, rc.Fixed, rc.Truth = rep.head, rep.base, rep.fixed, rep.truth
	loop, err := agentloop.New(model, clock.System{}, agentloop.Config{Model: o.model, MaxOutputTokens: maxOutputPerAnswer,
		BudgetUSD: budget, InputTokenCap: inputTokensPerCase, OutputTokenCap: outputTokensPerCas,
		Deadline: time.Now().Add(caseDeadline), MaxTurns: maxTurnsPerCase}, rep.tools(), []agentloop.Submitter{finishReview{}})
	if err != nil {
		rc.Error = "agent loop: " + err.Error()
		return rc, false
	}
	res, runErr := loop.Run(ctx, reviewerMessages(c, rep))
	rc.Stop, rc.Turns, rc.CostUSD = res.Reason, res.Turns, res.CostUSD
	rc.InputTokens, rc.CachedTokens, rc.OutputTokens = res.Usage.InputTokens, res.Usage.CachedInputTokens, res.Usage.OutputTokens
	if runErr != nil {
		rc.Error = runErr.Error()
		return rc, errors.Is(runErr, bench.ErrCostCap)
	}
	for i, p := range rep.proposals {
		rc.Findings = append(rc.Findings, rep.prove(ctx, i+1, p))
	}
	var truth []bench.FileTruth
	if rep.truth != nil {
		truth = rep.truth.Truth
	}
	score := bench.ScoreCase(c.Kind, truth, rc.Findings, o.blockAt)
	score.ID, score.Scored = c.ID, true
	score.CostUSD, score.InputTokens, score.CachedTokens, score.OutputTokens = rc.CostUSD, rc.InputTokens, rc.CachedTokens, rc.OutputTokens
	rc.CaseScore = score
	return rc, false
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
