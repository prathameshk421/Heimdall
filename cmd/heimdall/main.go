// Command heimdall.
//
// The command line: connect, admin, doctor, pack, eval and the MCP stdio bridge. Only doctor is built so far.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
)

// exitNotBuilt is the exit status of a subcommand whose task has not landed.
const exitNotBuilt = 2

func main() {
	if len(os.Args) > 1 {
		if run, ok := command(os.Args[1]); ok {
			os.Exit(run(os.Args[2:]))
		}
	}
	fmt.Fprintln(os.Stderr, "heimdall: not built yet")
	os.Exit(exitNotBuilt)
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
