package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/ledger"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/openrouter"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/budget"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

func defaultPurposeBudgets() map[string]float64 {
	return map[string]float64{"spikes": .2, "live": 1, "eval": 1, "rehearse": .6}
}

type budgetDeps struct {
	getenv     func(string) string
	loadConfig func(string) (config.Config, error)
	total      func(string) (float64, map[string]float64, error)
	balance    func(config.Config) (float64, error)
}

func runBudgetCheck(args []string, stdout, stderr io.Writer) int {
	return runBudgetCheckWithDeps(args, stdout, stderr, budgetDeps{
		getenv: config.Getenv, loadConfig: config.Load,
		total: func(path string) (float64, map[string]float64, error) {
			return (ledger.Ledger{Path: path}).Total(context.Background())
		},
		balance: func(cfg config.Config) (float64, error) {
			return (openrouter.Client{Secrets: costSecrets{cfg: cfg}, SecretName: openRouterFile}).Balance(context.Background())
		},
	})
}

func runBudgetCheckWithDeps(args []string, stdout, stderr io.Writer, deps budgetDeps) int {
	fs := flag.NewFlagSet("heimdall budget-check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var purpose, configPath, ledgerPath, estimateText string
	fs.StringVar(&purpose, "purpose", "", "budget purpose")
	fs.StringVar(&estimateText, "estimate", "", "estimated spend in USD")
	fs.StringVar(&configPath, "config", "", "path to heimdall.toml")
	fs.StringVar(&ledgerPath, "ledger", "testdata/cost-ledger.jsonl", "path to the committed cost ledger")
	if err := fs.Parse(args); err != nil {
		return doctorExitUsage
	}
	if fs.NArg() != 0 || strings.TrimSpace(purpose) == "" || strings.TrimSpace(estimateText) == "" {
		complain(stderr, "heimdall budget-check: purpose and estimate are required\n")
		return doctorExitUsage
	}
	estimate, err := strconv.ParseFloat(estimateText, 64)
	if err != nil || estimate < 0 {
		complain(stderr, "heimdall budget-check: estimate must be a non-negative number\n")
		return doctorExitUsage
	}
	var configArgs []string
	if configPath != "" {
		configArgs = []string{"--config", configPath}
	}
	path, err := config.FilePath(configArgs, deps.getenv)
	if err != nil {
		complain(stderr, "heimdall budget-check: %v\n", err)
		return doctorExitFail
	}
	cfg, err := deps.loadConfig(path)
	if err != nil {
		complain(stderr, "heimdall budget-check: load config: %v\n", err)
		return doctorExitFail
	}
	total, purposes, err := deps.total(ledgerPath)
	if err != nil {
		complain(stderr, "heimdall budget-check: read ledger: %v\n", err)
		return doctorExitFail
	}
	guard := budget.Guard{CapUSD: testingCapUSD, PerPurpose: defaultPurposeBudgets(), Ledger: budgetLedger{total: total, purposes: purposes}, Balance: budgetBalance{deps: deps, cfg: cfg}}
	if err := guard.Check(context.Background(), purpose, estimate); err != nil {
		complain(stderr, "heimdall budget-check: %v\n", err)
		return doctorExitFail
	}
	_, _ = fmt.Fprintln(stdout, "budget accepted")
	return 0
}

type budgetRunDeps struct {
	getenv     func(string) string
	loadConfig func(string) (config.Config, error)
	balance    func(config.Config) (float64, error)
	append     func(string, ports.LedgerEntry) error
	run        func(context.Context, []string, io.Writer, io.Writer) int
	now        func() time.Time
}

func runBudgetRun(args []string, stdout, stderr io.Writer) int {
	return runBudgetRunWithDeps(args, stdout, stderr, budgetRunDeps{
		getenv: config.Getenv, loadConfig: config.Load,
		balance: func(cfg config.Config) (float64, error) {
			return (openrouter.Client{Secrets: costSecrets{cfg: cfg}, SecretName: openRouterFile}).Balance(context.Background())
		},
		append: func(path string, entry ports.LedgerEntry) error {
			return (ledger.Ledger{Path: path}).AppendOnce(context.Background(), entry)
		},
		run: func(ctx context.Context, command []string, stdout, stderr io.Writer) int {
			if len(command) == 0 {
				return doctorExitUsage
			}
			cmd := exec.CommandContext(ctx, command[0], command[1:]...) // #nosec G204 -- command is the explicit tagged test wrapper
			cmd.Stdout = stdout
			cmd.Stderr = stderr
			if err := cmd.Run(); err != nil {
				var exit *exec.ExitError
				if errors.As(err, &exit) {
					return exit.ExitCode()
				}
				return doctorExitFail
			}
			return 0
		},
		now: time.Now,
	})
}

func runBudgetRunWithDeps(args []string, stdout, stderr io.Writer, deps budgetRunDeps) int {
	options, err := parseBudgetRunOptions(args, stderr)
	if err != nil {
		complain(stderr, "heimdall budget-run: %v\n", err)
		return doctorExitUsage
	}
	path, err := config.FilePath(options.configArgs(), deps.getenv)
	if err != nil {
		complain(stderr, "heimdall budget-run: %v\n", err)
		return doctorExitFail
	}
	cfg, err := deps.loadConfig(path)
	if err != nil {
		complain(stderr, "heimdall budget-run: load config: %v\n", err)
		return doctorExitFail
	}
	start, err := deps.balance(cfg)
	if err != nil {
		complain(stderr, "heimdall budget-run: read provider balance before run: %v\n", err)
		return doctorExitFail
	}
	if reserve := 2 * options.estimate; start < reserve {
		complain(stderr, "heimdall budget-run: live balance $%.4f is below 2x estimate $%.4f; run refused\n", start, reserve)
		return doctorExitFail
	}
	childCode := deps.run(context.Background(), options.command, stdout, stderr)
	end, err := deps.balance(cfg)
	if err != nil {
		complain(stderr, "heimdall budget-run: read provider balance after run: %v\n", err)
		return doctorExitFail
	}
	measured := start - end
	if measured < 0 {
		complain(stderr, "heimdall budget-run: provider balance increased during run; spend is not measurable\n")
		return doctorExitFail
	}
	entry := ports.LedgerEntry{
		RunID: options.runID, Purpose: options.purpose, Model: options.model, CostUSD: measured, At: deps.now(),
		Note: "measured from OpenRouter balance delta; token counts unavailable from the provider credits endpoint",
	}
	if err := deps.append(options.ledgerPath, entry); err != nil {
		complain(stderr, "heimdall budget-run: record run: %v\n", err)
		return doctorExitFail
	}
	_, _ = fmt.Fprintf(stdout, "recorded %s cost $%.4f\n", options.runID, measured)
	if budget.StopAfterTwiceEstimate(measured, options.estimate) {
		complain(stderr, "heimdall budget-run: stop rule reached: measured $%.4f is at least 2x estimate $%.4f\n", measured, options.estimate)
		return doctorExitFail
	}
	return childCode
}

type budgetRunOptions struct {
	purpose, task, model, runID, configPath, ledgerPath string
	estimate                                            float64
	command                                             []string
}

func (o budgetRunOptions) configArgs() []string {
	if o.configPath == "" {
		return nil
	}
	return []string{"--config", o.configPath}
}

func parseBudgetRunOptions(args []string, stderr io.Writer) (budgetRunOptions, error) {
	fs := flag.NewFlagSet("heimdall budget-run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var purpose, estimateText, task, model, runID, configPath, ledgerPath string
	fs.StringVar(&purpose, "purpose", "", "budget purpose")
	fs.StringVar(&estimateText, "estimate", "", "estimated spend in USD")
	fs.StringVar(&task, "task", "", "task id")
	fs.StringVar(&model, "model", "", "model identifier")
	fs.StringVar(&runID, "run-id", "", "stable run identifier for replay")
	fs.StringVar(&configPath, "config", "", "path to heimdall.toml")
	fs.StringVar(&ledgerPath, "ledger", "testdata/cost-ledger.jsonl", "path to the committed cost ledger")
	if err := fs.Parse(args); err != nil {
		return budgetRunOptions{}, err
	}
	if fs.NArg() == 0 || strings.TrimSpace(purpose) == "" || strings.TrimSpace(estimateText) == "" || strings.TrimSpace(task) == "" || strings.TrimSpace(model) == "" {
		return budgetRunOptions{}, errors.New("purpose, estimate, task, model and a command are required")
	}
	estimate, err := strconv.ParseFloat(estimateText, 64)
	if err != nil || estimate < 0 {
		return budgetRunOptions{}, errors.New("estimate must be a non-negative number")
	}
	if runID == "" {
		runID = fmt.Sprintf("run-%s-%d", task, time.Now().UnixNano())
	}
	return budgetRunOptions{purpose: purpose, task: task, model: model, runID: runID, configPath: configPath, ledgerPath: ledgerPath, estimate: estimate, command: fs.Args()}, nil
}

type budgetLedger struct {
	total    float64
	purposes map[string]float64
}

func (l budgetLedger) Total(context.Context) (float64, map[string]float64, error) {
	return l.total, l.purposes, nil
}

type budgetBalance struct {
	deps budgetDeps
	cfg  config.Config
}

func (b budgetBalance) Balance(context.Context) (float64, error) { return b.deps.balance(b.cfg) }
