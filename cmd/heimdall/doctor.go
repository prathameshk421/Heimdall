package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
)

const (
	statusOK      = "ok"
	statusFail    = "fail"
	statusSkipped = "skipped"

	// doctorExitFail is the status when any check fails. Scripts and the VM's `doctor --host` rely on it.
	doctorExitFail = 1
	// doctorExitUsage is the status for a bad command line, which prints no table.
	doctorExitUsage = 2
	// dockerTimeout bounds the docker call so a hung daemon cannot hang the command (CODING_STANDARDS 4).
	dockerTimeout = 10 * time.Second
)

// checkResult is one row of the doctor table.
type checkResult struct {
	Check  string `json:"check"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// doctorDeps is everything the checks touch outside this file. main wires the real ones; tests inject fakes.
type doctorDeps struct {
	getenv        func(name string) string
	loadConfig    func(path string) (config.Config, error)
	dockerVersion func(ctx context.Context) (string, error)
}

// runDoctor runs every check, prints the result as a table (or JSON with --json) and returns the exit
// status: 0 when no check fails, doctorExitFail when one does, doctorExitUsage for a bad command line.
// Every check runs even when an earlier one failed, so one run shows the whole picture.
func runDoctor(args []string, stdout, stderr io.Writer, deps doctorDeps) int {
	fs := flag.NewFlagSet("heimdall doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the checks as a JSON array")
	configPath := fs.String("config", "", "path to heimdall.toml (default: HEIMDALL_CONFIG)")
	if err := fs.Parse(args); err != nil {
		return doctorExitUsage
	}
	if fs.NArg() > 0 {
		complain(stderr, "heimdall doctor: unexpected argument %q\n", fs.Arg(0))
		return doctorExitUsage
	}

	results := []checkResult{
		checkConfig(*configPath, deps),
		checkDocker(deps),
		checkPostgres(),
	}
	failed := false
	for _, r := range results {
		failed = failed || r.Status == statusFail
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(results); err != nil {
			complain(stderr, "heimdall doctor: write JSON: %v\n", err)
			return doctorExitFail
		}
	} else if err := writeTable(stdout, results); err != nil {
		complain(stderr, "heimdall doctor: write table: %v\n", err)
		return doctorExitFail
	}
	if failed {
		return doctorExitFail
	}
	return 0
}

// checkConfig loads the config file named by --config or HEIMDALL_CONFIG. It uses config.FilePath so the
// rule for where the path comes from lives in one place.
func checkConfig(flagPath string, deps doctorDeps) checkResult {
	var flagArgs []string
	if flagPath != "" {
		flagArgs = []string{"--config", flagPath}
	}
	path, err := config.FilePath(flagArgs, deps.getenv)
	if err != nil {
		return result("config", statusFail, err.Error())
	}
	if _, err := deps.loadConfig(path); err != nil {
		return result("config", statusFail, err.Error())
	}
	return result("config", statusOK, path+" loads and validates")
}

// checkDocker asks the daemon for its version. It needs no secrets, only a reachable Docker.
func checkDocker(deps doctorDeps) checkResult {
	ctx, cancel := context.WithTimeout(context.Background(), dockerTimeout)
	defer cancel()
	version, err := deps.dockerVersion(ctx)
	if err != nil {
		return result("docker", statusFail, err.Error())
	}
	return result("docker", statusOK, "daemon reachable, server version "+version)
}

// checkPostgres is out of scope for T12: there is no DSN key in heimdall.toml yet, so the check cannot run.
func checkPostgres() checkResult {
	return result("postgres", statusSkipped, "no database DSN in heimdall.toml yet; the check lands with the Postgres adapter")
}

func result(check, status, detail string) checkResult {
	return checkResult{Check: check, Status: status, Detail: detail}
}

func writeTable(w io.Writer, results []checkResult) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "CHECK\tSTATUS\tDETAIL"); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	for _, r := range results {
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\n", r.Check, r.Status, r.Detail); err != nil {
			return fmt.Errorf("write row %s: %w", r.Check, err)
		}
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("flush table: %w", err)
	}
	return nil
}

// complain writes one line to stderr. When stderr itself cannot be written there is nowhere left to
// report, so the write error is dropped on purpose; the exit status still carries the outcome.
func complain(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}
