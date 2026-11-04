package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/github"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
)

const (
	statusOK      = "ok"
	statusFail    = "fail"
	statusSkipped = "skipped"
	// statusMissing is a permission the product needs that the installation does not grant at that level.
	statusMissing = "missing"
	// statusTooBroad is a permission the product does not need, or one granted above the level it needs. It is a
	// warning, and a failure only under --strict.
	statusTooBroad = "too broad"
	// statusNotVerified is a check that could not run, with the reason in its detail.
	statusNotVerified = "not verified"

	// doctorExitFail is the status when any check fails. Scripts and the VM's `doctor --host` rely on it.
	doctorExitFail = 1
	// doctorExitUsage is the status for a bad command line, which prints no table.
	doctorExitUsage = 2
	// dockerTimeout bounds the docker call so a hung daemon cannot hang the command (CODING_STANDARDS 4).
	dockerTimeout = 10 * time.Second
	// githubDoctorTimeout bounds the GitHub call that reads the installation, so a hung API cannot hang the command.
	githubDoctorTimeout = 30 * time.Second
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
	// githubReader builds the reader of the GitHub installation. It is used only when GitHub is the VCS provider.
	githubReader func(cfg config.Config) (installationReader, error)
}

// installationReader reads what the GitHub App's installation holds. The GitHub adapter's TokenSource satisfies it.
type installationReader interface {
	Installation(ctx context.Context) (github.Installation, error)
}

// runDoctor runs every check, prints the result as a table (or JSON with --json) and returns the exit
// status: 0 when no check fails, doctorExitFail when one does, doctorExitUsage for a bad command line.
// Every check runs even when an earlier one failed, so one run shows the whole picture.
func runDoctor(args []string, stdout, stderr io.Writer, deps doctorDeps) int {
	fs := flag.NewFlagSet("heimdall doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the checks as a JSON array")
	strict := fs.Bool("strict", false, "treat a too broad GitHub permission as a failure")
	configPath := fs.String("config", "", "path to heimdall.toml (default: HEIMDALL_CONFIG)")
	if err := fs.Parse(args); err != nil {
		return doctorExitUsage
	}
	if fs.NArg() > 0 {
		complain(stderr, "heimdall doctor: unexpected argument %q\n", fs.Arg(0))
		return doctorExitUsage
	}

	cfg, configRow := checkConfig(*configPath, deps)
	results := []checkResult{
		configRow,
		checkDocker(deps),
		checkPostgres(),
	}
	if provider, ok := cfg.Selection("vcs"); ok && provider == "github" {
		results = append(results, checkGitHub(deps, cfg)...)
	}
	failed := false
	for _, r := range results {
		failed = failed || blocks(r, *strict)
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

// blocks says whether a row makes the exit status fail. A too broad permission is a warning unless strict is set.
func blocks(r checkResult, strict bool) bool {
	switch r.Status {
	case statusFail, statusMissing, statusNotVerified:
		return true
	case statusTooBroad:
		return strict
	}
	return false
}

// checkConfig loads the config file named by --config or HEIMDALL_CONFIG. It uses config.FilePath so the
// rule for where the path comes from lives in one place. It returns the loaded config, or the zero value when
// the check fails.
func checkConfig(flagPath string, deps doctorDeps) (config.Config, checkResult) {
	var flagArgs []string
	if flagPath != "" {
		flagArgs = []string{"--config", flagPath}
	}
	path, err := config.FilePath(flagArgs, deps.getenv)
	if err != nil {
		return config.Config{}, result("config", statusFail, err.Error())
	}
	cfg, err := deps.loadConfig(path)
	if err != nil {
		return config.Config{}, result("config", statusFail, err.Error())
	}
	return cfg, result("config", statusOK, path+" loads and validates")
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

// checkGitHub reads the installation's real permissions and repositories and compares them with what the product
// needs. A reader that is not wired, cannot be built or fails to answer gives one not verified row with the reason.
func checkGitHub(deps doctorDeps, cfg config.Config) []checkResult {
	if deps.githubReader == nil {
		return []checkResult{result("github", statusNotVerified, "no GitHub reader is wired into doctor")}
	}
	reader, err := deps.githubReader(cfg)
	if err != nil {
		return []checkResult{result("github", statusNotVerified, err.Error())}
	}
	if reader == nil {
		return []checkResult{result("github", statusNotVerified, "the GitHub reader is nil")}
	}
	ctx, cancel := context.WithTimeout(context.Background(), githubDoctorTimeout)
	defer cancel()
	inst, err := reader.Installation(ctx)
	if err != nil {
		return []checkResult{result("github", statusNotVerified, err.Error())}
	}
	return compareInstallation(inst, github.RequiredPermissions())
}

// permissionRank orders the levels a GitHub App permission can hold: read 1, write 2, admin 3. An unknown level is 0.
func permissionRank(level string) int {
	switch level {
	case "read":
		return 1
	case "write":
		return 2
	case "admin":
		return 3
	}
	return 0
}

// compareInstallation returns one row per needed permission, then one row per permission the product does not
// need, then one row for the repositories. The rows come in a fixed order, so the output is the same every run.
func compareInstallation(inst github.Installation, need map[string]string) []checkResult {
	var rows []checkResult
	names := make([]string, 0, len(need))
	for name := range need {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		rows = append(rows, comparePermission(name, inst.Permissions[name], need[name]))
	}
	var extra []string
	for name := range inst.Permissions {
		if _, ok := need[name]; !ok {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		rows = append(rows, result("github."+name, statusTooBroad,
			fmt.Sprintf("granted %s, the product does not need it", inst.Permissions[name])))
	}
	rows = append(rows, compareRepositories(inst.Repositories))
	return rows
}

// comparePermission compares one granted level with the level the product needs. Absent is missing, a lower
// level is missing, and a higher level is too broad.
func comparePermission(name, granted, needed string) checkResult {
	check := "github." + name
	switch {
	case granted == "":
		return result(check, statusMissing, fmt.Sprintf("not granted; the product needs %s", needed))
	case permissionRank(granted) < permissionRank(needed):
		return result(check, statusMissing, fmt.Sprintf("has %s, needs %s", granted, needed))
	case permissionRank(granted) > permissionRank(needed):
		return result(check, statusTooBroad, fmt.Sprintf("has %s, %s is enough", granted, needed))
	}
	return result(check, statusOK, "has "+granted+", as needed")
}

// compareRepositories says which repositories the installation reaches. An installation that reaches none cannot
// do any work, so it is missing.
func compareRepositories(repos github.Repositories) checkResult {
	switch {
	case repos.All:
		return result("github.repositories", statusOK, "all repositories; narrowed tokens limit what Heimdall touches")
	case len(repos.Names) == 0:
		return result("github.repositories", statusMissing, "the installation reaches no repository")
	}
	return result("github.repositories", statusOK, "selected: "+strings.Join(repos.Names, ", "))
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
