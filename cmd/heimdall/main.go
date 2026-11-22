// Command heimdall.
//
// The command line: admin setup, connect, doctor, cost, the MCP stdio bridge, and the Action-mode steps (heimdall
// action trigger, proof, gate and init; ACTION_MODE 3.2, AM15).
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
)

// exitNotBuilt is the exit status of a subcommand whose task has not landed.
const exitNotBuilt = 2

func main() {
	os.Exit(runCLI(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func runCLI(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		writeCLIUsage(stdout)
		return 0
	}
	subcommand, rest := args[0], args[1:]
	switch subcommand {
	case "help", "-h", "--help":
		writeCLIUsage(stdout)
		return 0
	case "doctor":
		return runDoctor(rest, stdout, stderr, realDoctorDeps())
	case "connect":
		return runConnect(rest, stdin, stdout, stderr, realConnectDeps())
	case "admin":
		return runAdmin(rest, stdin, stdout, stderr)
	case "controller":
		return runController(rest, stdin, stdout, stderr, realControllerDeps())
	case "cards":
		return runCards(rest, stdin, stdout, stderr)
	case "action":
		return runAction(rest, stdin, stdout, stderr, realActionDeps())
	default:
		return runOutputOnly(subcommand, rest, stdout, stderr)
	}
}

// runOutputOnly runs the subcommands that need no stdin.
func runOutputOnly(subcommand string, rest []string, stdout, stderr io.Writer) int {
	switch subcommand {
	case "cost":
		return runCost(rest, stdout, stderr)
	case "budget-check":
		return runBudgetCheck(rest, stdout, stderr)
	case "budget-run":
		return runBudgetRun(rest, stdout, stderr)
	case "secrets-sync":
		return runSecretsSync(rest, stdout, stderr)
	case "switch-register":
		return runSwitchRegister(rest, stdout, stderr)
	default:
		writeDiagnostic(stderr, fmt.Sprintf("heimdall: unknown command %q\nRun 'heimdall --help' for usage.\n", subcommand))
		return exitNotBuilt
	}
}

func writeCLIUsage(w io.Writer) {
	_, _ = io.WriteString(w, `heimdall — evidence-based PR gate and company engineering review runtime

Action mode (zero-infra GitHub PR gating):
  heimdall action init -repo owner/name [-apply]   Bootstrap workflows, secrets, and shadow mode
  heimdall action trigger | proof | gate           Workflow steps (run by deploy/action)
  heimdall action digest                           Weekly shadow readiness and misses report

Company & connector setup (server mode):
  heimdall admin init [--config PATH]              Create heimdall.toml interactively
  heimdall admin secret ...                        Manage owner-only secret files
  heimdall connect list [--json] [--config PATH]   List VCS, tracker, chat, runtime, and memory connectors
  heimdall connect <kind> [name] [--config PATH]   Select & verify a connector (github, jira, switch, telegram, slack, discord, mattermost, teams, switchroom, graphiti)
  heimdall connect people ...                      Map human Switch sender IDs for chat commands
  heimdall connect progress [--config PATH]        Show connector setup progress
  heimdall switch-register ...                     Register coordinator agent with Switch
  heimdall controller enroll ...                   Enroll headless Switch agent controller
  heimdall doctor [--strict] [--json]              Verify host, Docker, config, and live connectors

Memory cards & cost:
  heimdall cards ...                               Scan, redact, approve, and DSSE-sign team memory cards
  heimdall cost | budget-check | budget-run        Inspect and enforce LLM spend budgets
  heimdall secrets-sync                            Sync host secret files safely
`)
}

func command(name string) (func([]string) int, bool) {
	commands := map[string]func([]string) int{
		"doctor":          func(args []string) int { return runDoctor(args, os.Stdout, os.Stderr, realDoctorDeps()) },
		"connect":         func(args []string) int { return runConnect(args, os.Stdin, os.Stdout, os.Stderr, realConnectDeps()) },
		"cost":            func(args []string) int { return runCost(args, os.Stdout, os.Stderr) },
		"budget-check":    func(args []string) int { return runBudgetCheck(args, os.Stdout, os.Stderr) },
		"budget-run":      func(args []string) int { return runBudgetRun(args, os.Stdout, os.Stderr) },
		"secrets-sync":    func(args []string) int { return runSecretsSync(args, os.Stdout, os.Stderr) },
		"switch-register": func(args []string) int { return runSwitchRegister(args, os.Stdout, os.Stderr) },
		"admin":           func(args []string) int { return runAdmin(args, os.Stdin, os.Stdout, os.Stderr) },
		"action":          func(args []string) int { return runAction(args, os.Stdin, os.Stdout, os.Stderr, realActionDeps()) },
		"cards":           func(args []string) int { return runCards(args, os.Stdin, os.Stdout, os.Stderr) },
		"controller": func(args []string) int {
			return runController(args, os.Stdin, os.Stdout, os.Stderr, realControllerDeps())
		},
	}
	run, ok := commands[name]
	return run, ok
}

// realDoctorDeps wires the doctor checks to the real environment, config loader and docker CLI.
func realDoctorDeps() doctorDeps {
	return doctorDeps{
		getenv:     config.Getenv,
		loadConfig: config.Load,
		hostChecks: realHostChecks(config.Getenv),
		integrationChecks: func(ctx context.Context, cfg config.Config) []checkResult {
			return configuredIntegrationChecks(ctx, cfg, defaultRegistry, connectCheckDeadline)
		},
		dockerVersion: func(ctx context.Context) (string, error) {
			out, err := exec.CommandContext(ctx, "docker", "version", "--format", "{{.Server.Version}}").CombinedOutput()
			text := strings.TrimSpace(string(out))
			if err != nil {
				return "", fmt.Errorf("docker version: %w: %s", err, text)
			}
			return text, nil
		},
	}
}
