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
	if len(os.Args) > 1 && os.Args[1] == "doctor" {
		os.Exit(runDoctor(os.Args[2:], os.Stdout, os.Stderr, realDoctorDeps()))
	}
	if len(os.Args) > 1 && os.Args[1] == "connect" {
		os.Exit(runConnect(os.Args[2:], os.Stdin, os.Stdout, os.Stderr, realConnectDeps()))
	}
	fmt.Fprintln(os.Stderr, "heimdall: not built yet")
	os.Exit(exitNotBuilt)
}

// realDoctorDeps wires the doctor checks to the real environment, config loader and docker CLI.
func realDoctorDeps() doctorDeps {
	return doctorDeps{
		getenv:     config.Getenv,
		loadConfig: config.Load,
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
