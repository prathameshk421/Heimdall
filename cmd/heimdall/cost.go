package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"
	"text/tabwriter"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/ledger"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/openrouter"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
)

const testingCapUSD = 3.0

const openRouterFile = "openrouter_api_key"

type costReport struct {
	TotalSpent float64            `json:"total_spent_usd"`
	Remaining  float64            `json:"remaining_usd"`
	ByPurpose  map[string]float64 `json:"per_purpose_usd"`
	Balance    float64            `json:"live_balance_usd"`
}

type costSecrets struct{ cfg config.Config }

func (s costSecrets) Get(name string) (string, error) { return s.cfg.Secret(name) }

type costDeps struct {
	getenv     func(string) string
	loadConfig func(string) (config.Config, error)
	total      func(string) (float64, map[string]float64, error)
	balance    func(config.Config) (float64, error)
}

type costOptions struct {
	json       bool
	configPath string
	ledgerPath string
}

func runCost(args []string, stdout, stderr io.Writer) int {
	return runCostWithDeps(args, stdout, stderr, costDeps{
		getenv:     config.Getenv,
		loadConfig: config.Load,
		total: func(path string) (float64, map[string]float64, error) {
			return (ledger.Ledger{Path: path}).Total(context.Background())
		},
		balance: func(cfg config.Config) (float64, error) {
			return (openrouter.Client{Secrets: costSecrets{cfg: cfg}, SecretName: openRouterFile}).Balance(context.Background())
		},
	})
}

func runCostWithDeps(args []string, stdout, stderr io.Writer, deps costDeps) int {
	options, err := parseCostOptions(args, stderr)
	if err != nil {
		return doctorExitUsage
	}
	report, err := loadCostReport(options, deps)
	if err != nil {
		complain(stderr, "heimdall cost: %v\n", err)
		return doctorExitFail
	}
	if err := writeCostReport(stdout, report, options.json); err != nil {
		complain(stderr, "heimdall cost: %v\n", err)
		return doctorExitFail
	}
	return 0
}

func parseCostOptions(args []string, stderr io.Writer) (costOptions, error) {
	fs := flag.NewFlagSet("heimdall cost", flag.ContinueOnError)
	fs.SetOutput(stderr)
	options := costOptions{}
	fs.BoolVar(&options.json, "json", false, "print JSON")
	fs.StringVar(&options.configPath, "config", "", "path to heimdall.toml (default: HEIMDALL_CONFIG)")
	fs.StringVar(&options.ledgerPath, "ledger", "testdata/cost-ledger.jsonl", "path to the committed cost ledger")
	if err := fs.Parse(args); err != nil {
		return costOptions{}, err
	}
	if fs.NArg() != 0 {
		return costOptions{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	return options, nil
}

func loadCostReport(options costOptions, deps costDeps) (costReport, error) {
	var configArgs []string
	if options.configPath != "" {
		configArgs = []string{"--config", options.configPath}
	}
	path, err := config.FilePath(configArgs, deps.getenv)
	if err != nil {
		return costReport{}, err
	}
	cfg, err := deps.loadConfig(path)
	if err != nil {
		return costReport{}, fmt.Errorf("load config: %w", err)
	}
	total, byPurpose, err := deps.total(options.ledgerPath)
	if err != nil {
		return costReport{}, err
	}
	balance, err := deps.balance(cfg)
	if err != nil {
		return costReport{}, fmt.Errorf("live balance: %w", err)
	}
	return costReport{TotalSpent: total, Remaining: testingCapUSD - total, ByPurpose: byPurpose, Balance: balance}, nil
}

func writeCostReport(stdout io.Writer, report costReport, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return fmt.Errorf("write JSON: %w", err)
		}
		return nil
	}
	w := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintf(w, "total spent\t$%.4f\nremaining to $3\t$%.4f\nlive balance\t$%.4f\n", report.TotalSpent, report.Remaining, report.Balance); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	purposes := make([]string, 0, len(report.ByPurpose))
	for purpose := range report.ByPurpose {
		purposes = append(purposes, purpose)
	}
	sort.Strings(purposes)
	for _, purpose := range purposes {
		if _, err := fmt.Fprintf(w, "%s\t$%.4f\n", purpose, report.ByPurpose[purpose]); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("flush output: %w", err)
	}
	return nil
}
