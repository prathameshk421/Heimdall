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
	case "skill":
		return runSkill(rest, stdout, stderr, os.UserHomeDir)
	case "version", "--version":
		writeDiagnostic(stdout, "heimdall "+versionString()+"\n")
		return 0
	default:
		writeDiagnostic(stderr, fmt.Sprintf("heimdall: unknown command %q\nRun 'heimdall --help' for usage.\n", subcommand))
		return exitNotBuilt
	}
}

func writeCLIUsage(w io.Writer) {
	_, _ = io.WriteString(w, `heimdall: a self-hosted agent harness for pull request review.
AI reviewers find issues, Heimdall proves each one with a failing test, and only proven findings block a merge.

Get started:
  heimdall action init -repo OWNER/REPO -dir CHECKOUT    Show the full setup plan for a GitHub repository
  heimdall action init ... -apply -model-key-stdin < F   Apply it: secrets, model settings, workflows, policy
  heimdall skill install                                 Install the setup skill for your coding agent
  heimdall doctor                                        Check this machine and every connection

GitHub Action mode (runs in your repository's CI):
  heimdall action init      Set up a repository (prints the plan; -apply runs it)
  heimdall action digest    Weekly report: what shadow mode would have blocked, and what it missed
  heimdall action trigger | proof | gate    Workflow steps; the workflows run them for you

Team mode (self-hosted server):
  heimdall admin init       Create heimdall.toml, or list the secret files still missing
  heimdall admin secret set Store one secret from a hidden prompt
  heimdall connect -list    List the connectors: GitHub, Jira, chat, agent runtime, memory
  heimdall connect KIND [NAME]   Select and check one connector (github, jira, slack, discord, telegram,
                                 mattermost, teams, switch, switchroom, graphiti)
  heimdall connect people   Map a person to their chat id, GitHub login and reviewer
  heimdall connect -check   Run every connector check
  heimdall switch-register  Register the coordinator agent with Switch
  heimdall controller enroll   Enroll the agent controller on the server
  heimdall doctor -host     Check the server: sudo, systemd, Docker, firewall, resources

Team memory and cost:
  heimdall cards ...        Scan notes, redact secrets, approve and sign team rule cards
  heimdall cost             Remaining model budget
  heimdall secrets-sync     Turn a .env file into owner-only secret files, printing no value

Other:
  heimdall skill [install [-dir DIR]]   Print the setup skill, or install it (default ~/.claude/skills)
  heimdall version          The commit this binary was built from

Docs: https://github.com/prathameshk421/Heimdall#readme
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
