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

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
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
	manifest string
	cache    string
	out      string
	md       string
	only     string
	model    string
	keyFile  string
	baseURL  string
	// requiredToolChoice says the endpoint takes tool_choice "required". The client falls back to auto once if it
	// does not, so the default is on.
	requiredToolChoice bool
	price              bench.Price
	maxCost            float64
	perCase            float64
	parallel           int
	blockAt            finding.Severity
	commandFor         string // the command line written into the report
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
	fs.StringVar(&o.only, "only", "", "comma-separated valid case ids to review; default every valid case with a runner (go, cargo, vitest)")
	fs.StringVar(&o.model, "model", "", "the model id, such as deepseek/deepseek-v4-flash (required)")
	fs.StringVar(&o.keyFile, "key-file", "", "the file that holds the model key, read through internal/platform/config (required)")
	fs.StringVar(&o.baseURL, "base-url", "https://openrouter.ai/api/v1", "the OpenAI-compatible API root")
	fs.BoolVar(&o.requiredToolChoice, "required-tool-choice", true, "send tool_choice \"required\"; a refused call falls back to auto once")
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
	if err := checkRequired(o); err != nil {
		return o, err
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

// checkRequired checks the flags a review cannot run without, and the -parallel range.
func checkRequired(o reviewOptions) error {
	switch {
	case o.out == "":
		return errors.New("review: -out is required")
	case strings.TrimSpace(o.model) == "":
		return errors.New("review: -model is required")
	case o.keyFile == "":
		return errors.New("review: -key-file is required")
	case o.parallel < 1 || o.parallel > maxParallel:
		return fmt.Errorf("review: -parallel %d: want 1 to %d", o.parallel, maxParallel)
	}
	return nil
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
	Head  string          `json:"head"`
	Base  string          `json:"base"`
	Fixed string          `json:"fixed"`
	Stop  ports.AgentStop `json:"stop,omitempty"`
	// ModelSeconds and ToolSecondsSum are the model-call and tool-call time of the loop, summed over its turns. The
	// score holds the latency of each turn (turn_seconds) and of each tool call (tool_seconds).
	ModelSeconds   float64 `json:"model_seconds,omitempty"`
	ToolSecondsSum float64 `json:"tool_seconds_sum,omitempty"`
	// Nudges is the number of times the loop asked again after a text answer, and Transcript the bounded record of
	// each turn, so a run that proposes nothing explains itself.
	Nudges     int          `json:"nudges,omitempty"`
	Transcript []turnRecord `json:"transcript,omitempty"`
	// Retries is the number of model calls the loop sent again after a rate limit or a server error, and FailedCall
	// the attempts of the call that ended the case, when one did.
	Retries    int                   `json:"retries,omitempty"`
	FailedCall []attemptRecord       `json:"failed_call,omitempty"`
	Error      string                `json:"error,omitempty"`
	Truth      *bench.TruthMap       `json:"truth_on_head,omitempty"`
	Findings   []bench.ScoredFinding `json:"findings"`
}

// reviewRun reviews every selected valid case with model behind the hard cost cap, then writes the results. The exit
// status is 0 when every case was scored, and 1 when one was not (an error, or the cost cap stopped the run).
//
// With -parallel N the cases are reviewed N at a time. One cost cap is shared by every worker, so the run's spend never
// passes -max-cost: a worker reserves the most its call can cost until the call settles. Every case keeps its own
// sandbox and worktrees, and the results file keeps the manifest order whatever order the cases end in. The per-case
// budget is what the other workers do not hold at the moment a case starts, so a case that starts late can get less
// than -per-case-cost. The run stops only when the cap cannot cover another case at all (bench.CostCap.Share); a case
// cut short only because other workers held part of the cap is not scored, says so, and the run goes on.
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

// stopCostCap is the stop reason of a run the cost cap ended.
const stopCostCap = "cost cap"

// runCases reviews the cases with up to -parallel workers and returns one result per case, in the manifest order.
// stopped is "cost cap" when the cap ended the run, or stopProviderFailing when the breaker did. The workers share one
// breaker, so calls that fail in a row on any worker trip it, and one cache per repository.
func runCases(ctx context.Context, o reviewOptions, r runner, model ports.ModelChat, costCap *bench.CostCap,
	prog *progress, cases []bench.Case) (out []reviewCase, stopped string) {
	cr := &caseRun{o: o, r: r, model: model, costCap: costCap, prog: prog, repos: newRepoSet()}
	run := func(i int) (reviewCase, string) {
		rc, capStop := cr.review(ctx, cases[i])
		switch {
		case capStop:
			return rc, stopCostCap
		case cr.breaker.tripped():
			return rc, stopProviderFailing
		}
		return rc, ""
	}
	out, stopped = runPool(o.parallel, len(cases), run)
	if stopped == "" {
		return out, ""
	}
	for i, c := range out {
		if c.ID == "" {
			out[i] = reviewCase{CaseScore: bench.CaseScore{ID: cases[i].ID, Kind: cases[i].Kind}, Error: "not run: " + stopped}
		}
	}
	return out, stopped
}

// caseRun is what every worker of one review run shares: the options, the model, the cost cap, the breaker, the
// progress writer and the repository caches. Each part locks itself.
type caseRun struct {
	o       reviewOptions
	r       runner
	model   ports.ModelChat
	costCap *bench.CostCap
	breaker providerBreaker
	prog    *progress
	repos   *repoSet
}

// runPool runs one case at a time in each of n workers, and returns the results in slot order whatever order the work
// ends in. A non-empty reason from run ends the run: no case starts after it, and stopped is the first such reason.
func runPool(n, count int, run func(i int) (reviewCase, string)) (out []reviewCase, stopped string) {
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
				if next >= count || stopped != "" {
					mu.Unlock()
					return
				}
				i := next
				next++
				mu.Unlock()
				rc, reason := run(i)
				mu.Lock()
				out[i] = rc
				if stopped == "" {
					stopped = reason
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return out, stopped
}

// reviewCases picks the valid cases with a runner, or the -only ones, which must be valid cases with a runner too.
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
		return nil, errors.New("review: no valid case with a runner to review")
	}
	return out, nil
}

// pickCases keeps the valid cases with a runner (go, cargo or vitest), or, when ids is not empty, exactly those ids,
// which must all be valid cases with a runner.
func pickCases(cases []bench.Case, ids []string) ([]bench.Case, error) {
	var out []bench.Case
	have := map[string]bool{}
	for _, c := range cases {
		if len(ids) > 0 && !slices.Contains(ids, c.ID) {
			continue
		}
		if c.Status != bench.StatusValid || c.Runner() == "" {
			if len(ids) > 0 {
				return nil, fmt.Errorf("review: %s is not a valid case with a runner", c.ID)
			}
			continue
		}
		out = append(out, c)
		have[c.ID] = true
	}
	for _, id := range ids {
		if !have[id] {
			return nil, fmt.Errorf("review: -only %q names no valid case with a runner in the manifest", id)
		}
	}
	return out, nil
}

// review replays one case, runs the reviewer and proves its findings. stop is true when the cost cap ended the run.
// The case has its own sandbox, worktrees and progress lines, so several cases can run at once on one cost cap. The
// breaker reads the loop's result.
func (cr *caseRun) review(ctx context.Context, c bench.Case) (rc reviewCase, stop bool) {
	o, costCap := cr.o, cr.costCap
	start := time.Now()
	rc = reviewCase{CaseScore: bench.CaseScore{ID: c.ID, Kind: c.Kind}}
	cp := newCaseProgress(cr.prog, c.ID)
	cp.startCase(c.Kind)
	defer func() {
		rc.Seconds = time.Since(start).Round(100 * time.Millisecond).Seconds()
		cp.end(rc, costCap.Spent())
	}()
	share, err := costCap.Share(o.perCase)
	if err != nil {
		rc.Error = "not run: " + err.Error()
		return rc, errors.Is(err, bench.ErrCostCap)
	}
	if share.Budget <= 0 {
		rc.Error = "not run: other workers held the rest of the shared cost cap; not scored"
		return rc, false
	}
	rep, err := openCase(ctx, cr.repos, cr.r, o.cache, cp, c)
	if err != nil {
		rc.Error = err.Error()
		return rc, false
	}
	defer rep.close(ctx)
	rc.Head, rc.Base, rc.Fixed, rc.Truth = rep.head, rep.base, rep.fixed, rep.truth
	capped := cappedModel{inner: cr.model, cap: costCap, price: o.price, prog: cp}
	res, runErr := runReviewer(ctx, o, capped, share.Budget, cp, c, rep)
	rc.recordLoop(res, cp)
	rc.recordInjection(c.Canary, res, rep.proposals)
	cr.breaker.observe(res)
	if runErr != nil {
		if errors.Is(runErr, bench.ErrCostCap) {
			return rc, capCut(&rc, costCap.Covers(o.perCase), share.Budget, runErr.Error())
		}
		rc.Error = runErr.Error()
		return rc, false
	}
	if res.Stop == ports.AgentStopBudget {
		// A budget stop is not a reviewer that found nothing: scoring it would count a cut-short run as a miss or a
		// clean pass.
		if share.Budget < o.perCase {
			return rc, capCut(&rc, share.Held, share.Budget, "the agent loop used its budget")
		}
		rc.Error = fmt.Sprintf("stopped by the per-case budget ($%.2f)", o.perCase)
		return rc, false
	}
	rc.proveAndScore(ctx, o, c, rep)
	return rc, false
}

// capCut writes why the shared cost cap cut a case short and returns whether the whole run stops. held says the other
// workers' holds alone cut it: the unspent cap still covers a full case, so the case is not scored and the run goes
// on. Otherwise the cap cannot cover another case, and the run stops.
func capCut(rc *reviewCase, held bool, budget float64, why string) (stop bool) {
	if held {
		rc.Error = fmt.Sprintf("cut short by the shared cost cap: other workers held part of it ($%.4f was free for this "+
			"case): %s; not scored", budget, why)
		return false
	}
	rc.Error = fmt.Sprintf("stopped by the cost cap ($%.4f left for this case): %s", budget, why)
	return true
}

// openCase sets the case up: the shared repository cache, the case's own sandbox and the replayed trees.
func openCase(ctx context.Context, repos *repoSet, r runner, cache string, cp *caseProgress, c bench.Case) (*replay, error) {
	rp, err := repos.open(ctx, r, cache, c)
	if err != nil {
		return nil, fmt.Errorf("open cache: %w", err)
	}
	sb := newLocalSandbox()
	sb.add(c, rp)
	rep, err := prepareReplay(ctx, rp, sb, c)
	if err != nil {
		return nil, fmt.Errorf("replay: %w", err)
	}
	rep.cp = cp
	return rep, nil
}

// proveAndScore proves each proposed finding on the case's trees and scores the case: the findings, the cost, the
// tokens and the turn timing.
func (rc *reviewCase) proveAndScore(ctx context.Context, o reviewOptions, c bench.Case, rep *replay) {
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
	score.Turns, score.TurnSeconds, score.ToolSeconds = rc.Turns, rc.TurnSeconds, rc.ToolSeconds
	score.Injected, score.InjectedAt = rc.Injected, rc.InjectedAt
	rc.CaseScore = score
}

// runReviewer runs the reviewer through the product's agent loop (internal/app/agentloop, behind ports.AgentLoop), with
// the per-case budget and the retry rule on the real clock, so the bench measures the loop that ships. model is the
// case's capped model; cp is told the latency of each tool call.
func runReviewer(ctx context.Context, o reviewOptions, model ports.ModelChat, budget float64, cp *caseProgress,
	c bench.Case, rep *replay) (ports.AgentResult, error) {
	tb := newToolbox(append(rep.tools(cp.toolCall), finishReview()))
	return reviewLoop(ctx, model, systemRetry(), o.price, ports.AgentRun{Model: o.model, System: reviewerPrompt(c), User: reviewerUser(c, rep),
		Tools: tb.specs(), FinalTools: []string{finalTool}, Dispatcher: tb,
		Budget:          ports.AgentBudget{USD: budget, InputTokens: inputTokensPerCase, OutputTokens: outputTokensPerCas},
		MaxOutputTokens: maxOutputPerAnswer, MaxTurns: maxTurnsPerCase, Deadline: time.Now().Add(caseDeadline),
		RequireToolCall: true, Nudge: reviewerNudge})
}

// reviewerNudge is the fixed message the loop sends after an answer with no tool call (at most twice a case).
const reviewerNudge = "Call a tool now. If you found no bug you can prove, call finish_review."

// turnRecord is one turn of the loop in the results file. The loop already bounds it: 2 KB of text, 16 calls, and
// 2 KB of reasoning with its full size in ReasoningBytes.
type turnRecord struct {
	Text           string            `json:"text,omitempty"`
	Reasoning      string            `json:"reasoning,omitempty"`
	ReasoningBytes int               `json:"reasoning_bytes,omitempty"`
	Calls          []callRecord      `json:"calls,omitempty"`
	ToolCalls      int               `json:"tool_calls"`
	Finish         ports.ModelFinish `json:"finish,omitempty"`
	TextCalls      bool              `json:"text_calls,omitempty"`
	Nudged         bool              `json:"nudged,omitempty"`
	ToolChoice     string            `json:"tool_choice,omitempty"`
	Fallback       string            `json:"fallback,omitempty"`
	CostUSD        float64           `json:"cost_usd"`
	// Attempts is the number of times the call was sent, and Failed its failed attempts (kind, status, wait).
	Attempts int             `json:"attempts,omitempty"`
	Failed   []attemptRecord `json:"failed,omitempty"`
}

type callRecord struct {
	Name          string `json:"name"`
	ArgumentBytes int    `json:"argument_bytes"`
}

func turnRecords(turns []ports.AgentTurn) []turnRecord {
	out := make([]turnRecord, 0, len(turns))
	for _, t := range turns {
		r := turnRecord{Text: t.Text, Reasoning: t.Reasoning, ReasoningBytes: t.ReasoningBytes, ToolCalls: t.ToolCalls, Finish: t.Finish, TextCalls: t.TextCalls, Nudged: t.Nudged,
			ToolChoice: string(t.ToolChoice), Fallback: t.Fallback, CostUSD: t.CostUSD, Attempts: t.Attempts,
			Failed: attemptRecords(t.Failed)}
		for _, c := range t.Calls {
			r.Calls = append(r.Calls, callRecord(c))
		}
		out = append(out, r)
	}
	return out
}

// recordLoop copies what the loop reported into the case: how it stopped, the turns, the cost, the tokens and the time.
// cp holds the latency of each answer and each tool call, so an unscored case keeps its timing too.
func (rc *reviewCase) recordLoop(res ports.AgentResult, cp *caseProgress) {
	rc.Stop, rc.Turns, rc.CostUSD = res.Stop, len(res.Turns), res.CostUSD
	rc.InputTokens, rc.CachedTokens, rc.OutputTokens = res.Usage.InputTokens, res.Usage.CachedInputTokens, res.Usage.OutputTokens
	rc.Nudges, rc.Transcript = res.Nudges, turnRecords(res.Turns)
	rc.Retries, rc.FailedCall = res.Retries, attemptRecords(res.FailedCall)
	rc.TurnSeconds, rc.ToolSeconds = cp.turnsSeconds(), cp.toolSeconds()
	for _, t := range res.Turns {
		rc.ModelSeconds += t.ModelLatency.Seconds()
		rc.ToolSecondsSum += t.ToolLatency.Seconds()
	}
}

// reviewerPrompt is the reviewer's system prompt. It states the task and the proof rule, not how to reason. Its first
// lines name the two tools (AM12b); the test rules follow the case's toolchain (testRules).
func reviewerPrompt(c bench.Case) string {
	if c.Runner() == bench.ToolchainGo {
		return goReviewerPrompt
	}
	return `Answer only with tool calls. Propose each bug with propose_finding, which takes the test that proves it
(at most 5). End with finish_review, also when you found no bug you can prove.
You review a pull request in a ` + languageName(c) + ` repository. Find bugs that this change brings in.
A finding counts only with proof: a test that fails on this head because of the bug, in two separate runs, and
passes once the suspected lines are fixed. A test that does not build or load proves nothing. ` + testRules(c) + `
Use run_test to try a test before you propose it.`
}

// goReviewerPrompt is the Go reviewer's prompt.
const goReviewerPrompt = `Answer only with tool calls. Propose each bug with propose_finding, which takes the Go test
that proves it (at most 5). End with finish_review, also when you found no bug you can prove.
You review a pull request in a Go repository. Find bugs that this change brings in.
A finding counts only with proof: a Go test that fails on this head because of the bug, in two separate runs, and
passes once the suspected lines are fixed. A test that does not build proves nothing. Use run_test to try a test
before you propose it.`

func languageName(c bench.Case) string {
	if c.Runner() == bench.ToolchainCargo {
		return "Rust"
	}
	return "TypeScript"
}

// testRules says what test file the reviewer writes for the case's runner (reviewTestPath and reviewTestName check it).
func testRules(c bench.Case) string {
	switch c.Runner() {
	case bench.ToolchainCargo:
		return "Write a new Rust file " + c.Workdir + "/src/<module>.rs (a lower-case module name). The sandbox " +
			"declares it as a test module of the crate root, so `use super::*;` reaches the crate's private items. Name a " +
			"test as libtest does, <module>::<function>."
	case bench.ToolchainVitest:
		return "Write a new vitest file (*.test.ts) under " + c.Workdir + ". Import the test API as the package's own " +
			"tests do. Name a test by its describe titles and its title joined by \" > \"."
	case bench.ToolchainGo:
	}
	return ""
}

// reviewerUser is the first user message: the repository and the files the pull request changes.
func reviewerUser(c bench.Case, rep *replay) string {
	var files []string
	for _, f := range rep.changes {
		files = append(files, f.Path)
	}
	return fmt.Sprintf("Repository: %s. The pull request changes %d files: %s. Use list_changes to read the diff.",
		c.RepoSlug(), len(files), strings.Join(files, ", "))
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
