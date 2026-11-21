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
	blockAt            finding.Severity
	commandFor         string // the command line written into the report
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
	fs.BoolVar(&o.requiredToolChoice, "required-tool-choice", true, "send tool_choice \"required\"; a refused call falls back to auto once")
	fs.Float64Var(&o.price.InputPerM, "price-input", 0, "USD per million input tokens; default the table's price for -model")
	fs.Float64Var(&o.price.CachedInputPerM, "price-cached", -1, "USD per million cache-read tokens; unset means the input price")
	fs.Float64Var(&o.price.OutputPerM, "price-output", 0, "USD per million output tokens; default the table's price for -model")
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
	if strings.TrimSpace(o.model) == "" {
		return o, errors.New("review: -model is required")
	}
	if o.keyFile == "" {
		return o, errors.New("review: -key-file is required")
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
	Head  string          `json:"head"`
	Base  string          `json:"base"`
	Fixed string          `json:"fixed"`
	Stop  ports.AgentStop `json:"stop,omitempty"`
	Turns int             `json:"turns"`
	// ModelSeconds and ToolSeconds are the model-call and tool-call time of the loop, summed over its turns.
	ModelSeconds float64 `json:"model_seconds,omitempty"`
	ToolSeconds  float64 `json:"tool_seconds,omitempty"`
	// Nudges is the number of times the loop asked again after a text answer, and Transcript the bounded record of
	// each turn, so a run that proposes nothing explains itself.
	Nudges     int                   `json:"nudges,omitempty"`
	Transcript []turnRecord          `json:"transcript,omitempty"`
	Error      string                `json:"error,omitempty"`
	Truth      *bench.TruthMap       `json:"truth_on_head,omitempty"`
	Findings   []bench.ScoredFinding `json:"findings"`
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
	res, runErr := runReviewer(ctx, o, model, budget, c, rep)
	rc.recordLoop(res)
	if runErr != nil {
		rc.Error = runErr.Error()
		return rc, errors.Is(runErr, bench.ErrCostCap)
	}
	if res.Stop == ports.AgentStopBudget {
		// A budget stop is not a reviewer that found nothing: scoring it would count a cut-short run as a miss or a
		// clean pass. When the run's cost cap, not the per-case budget, set this case's budget, the run stops too.
		if budget < o.perCase {
			rc.Error = fmt.Sprintf("stopped by the cost cap ($%.4f left for this case)", budget)
			return rc, true
		}
		rc.Error = fmt.Sprintf("stopped by the per-case budget ($%.2f)", o.perCase)
		return rc, false
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

// runReviewer runs the reviewer through the product's agent loop (internal/app/agentloop, behind ports.AgentLoop), with
// the per-case budget, so the bench measures the loop that ships.
func runReviewer(ctx context.Context, o reviewOptions, model ports.ModelChat, budget float64, c bench.Case, rep *replay) (ports.AgentResult, error) {
	loop, err := agentloop.NewRunner(model, clock.System{}, ports.ModelPrice{InputPerM: o.price.InputPerM,
		CachedInputPerM: o.price.CachedInputPerM, OutputPerM: o.price.OutputPerM})
	if err != nil {
		return ports.AgentResult{}, fmt.Errorf("agent loop: %w", err)
	}
	tb := newToolbox(append(rep.tools(), finishReview()))
	return loop.Run(ctx, ports.AgentRun{Model: o.model, System: reviewerPrompt, User: reviewerUser(c, rep),
		Tools: tb.specs(), FinalTools: []string{finalTool}, Dispatcher: tb,
		Budget:          ports.AgentBudget{USD: budget, InputTokens: inputTokensPerCase, OutputTokens: outputTokensPerCas},
		MaxOutputTokens: maxOutputPerAnswer, MaxTurns: maxTurnsPerCase, Deadline: time.Now().Add(caseDeadline),
		RequireToolCall: true, Nudge: reviewerNudge})
}

// reviewerNudge is the fixed message the loop sends after an answer with no tool call (at most twice a case).
const reviewerNudge = "Call a tool now. If you found no bug you can prove, call finish_review."

// turnRecord is one turn of the loop in the results file. The loop already bounds it: 2 KB of text, 16 calls.
type turnRecord struct {
	Text       string            `json:"text,omitempty"`
	Calls      []callRecord      `json:"calls,omitempty"`
	ToolCalls  int               `json:"tool_calls"`
	Finish     ports.ModelFinish `json:"finish,omitempty"`
	TextCalls  bool              `json:"text_calls,omitempty"`
	Nudged     bool              `json:"nudged,omitempty"`
	ToolChoice string            `json:"tool_choice,omitempty"`
	Fallback   string            `json:"fallback,omitempty"`
	CostUSD    float64           `json:"cost_usd"`
}

type callRecord struct {
	Name          string `json:"name"`
	ArgumentBytes int    `json:"argument_bytes"`
}

func turnRecords(turns []ports.AgentTurn) []turnRecord {
	out := make([]turnRecord, 0, len(turns))
	for _, t := range turns {
		r := turnRecord{Text: t.Text, ToolCalls: t.ToolCalls, Finish: t.Finish, TextCalls: t.TextCalls, Nudged: t.Nudged,
			ToolChoice: string(t.ToolChoice), Fallback: t.Fallback, CostUSD: t.CostUSD}
		for _, c := range t.Calls {
			r.Calls = append(r.Calls, callRecord(c))
		}
		out = append(out, r)
	}
	return out
}

// recordLoop copies what the loop reported into the case: how it stopped, the turns, the cost, the tokens and the time.
func (rc *reviewCase) recordLoop(res ports.AgentResult) {
	rc.Stop, rc.Turns, rc.CostUSD = res.Stop, len(res.Turns), res.CostUSD
	rc.InputTokens, rc.CachedTokens, rc.OutputTokens = res.Usage.InputTokens, res.Usage.CachedInputTokens, res.Usage.OutputTokens
	rc.Nudges, rc.Transcript = res.Nudges, turnRecords(res.Turns)
	for _, t := range res.Turns {
		rc.ModelSeconds += t.ModelLatency.Seconds()
		rc.ToolSeconds += t.ToolLatency.Seconds()
	}
}

// reviewerPrompt is the reviewer's system prompt. It states the task and the proof rule, not how to reason.
const reviewerPrompt = `Answer only with tool calls. Propose each bug with propose_finding, which takes the Go test
that proves it (at most 5). End with finish_review, also when you found no bug you can prove.
You review a pull request in a Go repository. Find bugs that this change brings in.
A finding counts only with proof: a Go test that fails on this head because of the bug, in two separate runs, and
passes once the suspected lines are fixed. A test that does not build proves nothing. Use run_test to try a test
before you propose it.`

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
