// Command heimdall.
//
// The command line: admin setup, connect, doctor, cost and the MCP stdio bridge.
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
		writeDiagnostic(stderr, "heimdall: not built yet\n")
		return exitNotBuilt
	}
	subcommand, rest := args[0], args[1:]
	switch subcommand {
	case "doctor":
		return runDoctor(rest, stdout, stderr, realDoctorDeps())
	case "connect":
		return runConnect(rest, stdin, stdout, stderr, realConnectDeps())
	case "admin":
		return runAdmin(rest, stdin, stdout, stderr)
	case "controller":
		return runController(rest, stdin, stdout, stderr, realControllerDeps())
	case "cost":
		return runCost(rest, stdout, stderr)
	case "budget-check":
		return runBudgetCheck(rest, stdout, stderr)
	case "budget-run":
		return runBudgetRun(rest, stdout, stderr)
	case "secrets-sync":
		return runSecretsSync(rest, stdout, stderr)
	default:
		writeDiagnostic(stderr, "heimdall: not built yet\n")
		return exitNotBuilt
	}
}

func command(name string) (func([]string) int, bool) {
	commands := map[string]func([]string) int{
		"doctor":       func(args []string) int { return runDoctor(args, os.Stdout, os.Stderr, realDoctorDeps()) },
		"connect":      func(args []string) int { return runConnect(args, os.Stdin, os.Stdout, os.Stderr, realConnectDeps()) },
		"cost":         func(args []string) int { return runCost(args, os.Stdout, os.Stderr) },
		"budget-check": func(args []string) int { return runBudgetCheck(args, os.Stdout, os.Stderr) },
		"budget-run":   func(args []string) int { return runBudgetRun(args, os.Stdout, os.Stderr) },
		"secrets-sync": func(args []string) int { return runSecretsSync(args, os.Stdout, os.Stderr) },
		"admin":        func(args []string) int { return runAdmin(args, os.Stdin, os.Stdout, os.Stderr) },
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
