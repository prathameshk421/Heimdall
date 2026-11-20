// Command heimdall-bench checks the Proof Bench dataset (internal/bench). It makes no model calls and never writes to
// GitHub: it only fetches commits read-only and runs git and go test locally.
//
//	heimdall-bench validate -out results.json [-manifest testdata/bench/cases.yaml] [-only id,id] [-cache dir]
//	heimdall-bench truth -case id [-manifest ...] [-cache dir]
//
// validate proves each case real (internal/bench doc.go) and writes one JSON results file. Its exit status is 0 when
// every case agrees with its manifest status, 1 when one does not, and 2 on a usage or manifest error.
// truth prints the truth block of a bug case, computed from its fix diff, as YAML for the manifest.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
)

// timeoutGrace is how long after go test's own timeout the process is killed.
const timeoutGrace = time.Minute

const (
	exitAgrees   = 0
	exitDisagree = 1
	exitUsage    = 2
)

func main() {
	os.Exit(runCLI(context.Background(), os.Args[1:], os.Stdout, os.Stderr, execRunner{}))
}

func runCLI(ctx context.Context, args []string, stdout, stderr io.Writer, r runner) int {
	if len(args) == 0 {
		writeDiagnostic(stderr, "usage: heimdall-bench validate|truth [flags]\n")
		return exitUsage
	}
	var code int
	var err error
	switch args[0] {
	case "validate":
		code, err = runValidate(ctx, args[1:], stderr, r)
	case "truth":
		code, err = runTruth(ctx, args[1:], stdout, stderr, r)
	default:
		code, err = exitUsage, fmt.Errorf("unknown subcommand %q", args[0])
	}
	if err != nil {
		writeDiagnostic(stderr, "heimdall-bench: "+err.Error()+"\n")
	}
	return code
}

type commonFlags struct {
	manifest string
	cache    string
}

func addCommon(fs *flag.FlagSet) *commonFlags {
	c := &commonFlags{}
	fs.StringVar(&c.manifest, "manifest", "testdata/bench/cases.yaml", "the dataset manifest")
	fs.StringVar(&c.cache, "cache", filepath.Join(os.TempDir(), "heimdall-bench"), "the repository cache directory")
	return c
}

func loadManifest(path string) (bench.Manifest, error) {
	b, err := os.ReadFile(path) // #nosec G304 -- the operator names the manifest file
	if err != nil {
		return bench.Manifest{}, fmt.Errorf("read manifest: %w", err)
	}
	return bench.Parse(b)
}

// report is the results file.
type report struct {
	HeimdallCommit string       `json:"heimdall_commit"`
	Started        string       `json:"started"`
	Machine        string       `json:"machine"`
	GoVersion      string       `json:"go_version"`
	Manifest       string       `json:"manifest"`
	Seconds        float64      `json:"seconds"`
	Cases          []caseResult `json:"cases"`
}

func runValidate(ctx context.Context, args []string, stderr io.Writer, r runner) (int, error) {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	common := addCommon(fs)
	out := fs.String("out", "", "write the JSON results to this file (required)")
	only := fs.String("only", "", "comma-separated case ids to check; default all")
	if err := fs.Parse(args); err != nil {
		return exitUsage, err
	}
	if *out == "" {
		return exitUsage, errors.New("validate: -out is required")
	}
	m, err := loadManifest(common.manifest)
	if err != nil {
		return exitUsage, err
	}
	cases, err := selectCases(m, *only)
	if err != nil {
		return exitUsage, err
	}
	start := time.Now()
	rep := newReport(ctx, r, common.manifest, start)
	code := exitAgrees
	for _, c := range cases {
		res := checkCase(ctx, r, common.cache, c)
		rep.Cases = append(rep.Cases, res)
		if res.Manifest != "agrees" {
			code = exitDisagree
		}
		writeDiagnostic(stderr, fmt.Sprintf("%-28s %-8s %6.1fs  %s  [manifest: %s]\n",
			c.ID, res.Outcome, res.Seconds, res.Reason, res.Manifest))
	}
	rep.Seconds = time.Since(start).Seconds()
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return exitUsage, fmt.Errorf("encode results: %w", err)
	}
	if err := os.WriteFile(*out, append(b, '\n'), 0o600); err != nil {
		return exitUsage, fmt.Errorf("write results: %w", err)
	}
	return code, nil
}

func selectCases(m bench.Manifest, only string) ([]bench.Case, error) {
	if only == "" {
		return m.Cases, nil
	}
	ids := strings.Split(only, ",")
	var out []bench.Case
	for _, c := range m.Cases {
		if slices.Contains(ids, c.ID) {
			out = append(out, c)
		}
	}
	if len(out) != len(ids) {
		return nil, fmt.Errorf("-only %q names a case that is not in the manifest", only)
	}
	return out, nil
}

func newReport(ctx context.Context, r runner, manifest string, start time.Time) report {
	rep := report{Started: start.UTC().Format(time.RFC3339), Manifest: manifest,
		Machine: fmt.Sprintf("%s/%s, %d CPUs", runtime.GOOS, runtime.GOARCH, runtime.NumCPU())}
	if out, _, err := r.run(ctx, "", "git", "rev-parse", "HEAD"); err == nil {
		rep.HeimdallCommit = strings.TrimSpace(string(out))
	} else {
		rep.HeimdallCommit = "unknown: " + err.Error()
	}
	if out, _, err := r.run(ctx, "", "go", "env", "GOVERSION"); err == nil {
		rep.GoVersion = strings.TrimSpace(string(out))
	} else {
		rep.GoVersion = "unknown: " + err.Error()
	}
	return rep
}

// checkCase runs the check for one case's kind and reconciles the outcome with the manifest status.
func checkCase(ctx context.Context, r runner, cache string, c bench.Case) caseResult {
	start := time.Now()
	res := runCheck(ctx, r, cache, c)
	res.ID, res.Kind, res.Language = c.ID, c.Kind, c.Language
	res.Seconds = time.Since(start).Round(100 * time.Millisecond).Seconds()
	res.Manifest = "agrees"
	if err := bench.Reconcile(c.Status, res.Outcome); err != nil {
		res.Manifest = err.Error()
	}
	return res
}

func runCheck(ctx context.Context, r runner, cache string, c bench.Case) caseResult {
	if c.Language != "go" {
		return caseResult{Outcome: bench.OutcomeSkipped, Reason: "no runner for " + c.Language + " yet"}
	}
	rp, err := openRepo(ctx, r, cache, c)
	if err != nil {
		return errorf("open cache: %v", err)
	}
	if c.Kind == bench.KindBug {
		return checkBug(ctx, rp, c)
	}
	return checkClean(ctx, rp, c)
}

// writeDiagnostic writes a progress or error line. A failed write to stderr has nowhere better to go.
func writeDiagnostic(w io.Writer, message string) { _, _ = io.WriteString(w, message) }

func runTruth(ctx context.Context, args []string, stdout, stderr io.Writer, r runner) (int, error) {
	fs := flag.NewFlagSet("truth", flag.ContinueOnError)
	fs.SetOutput(stderr)
	common := addCommon(fs)
	id := fs.String("case", "", "the bug case id (required)")
	if err := fs.Parse(args); err != nil {
		return exitUsage, err
	}
	m, err := loadManifest(common.manifest)
	if err != nil {
		return exitUsage, err
	}
	if *id == "" || strings.Contains(*id, ",") {
		return exitUsage, errors.New("truth: -case must name one case")
	}
	cases, err := selectCases(m, *id)
	if err != nil {
		return exitUsage, err
	}
	c := cases[0]
	if c.Kind != bench.KindBug || c.Language != "go" {
		return exitUsage, fmt.Errorf("truth: %s is not a Go bug case", c.ID)
	}
	rp, err := openRepo(ctx, r, common.cache, c)
	if err != nil {
		return exitDisagree, err
	}
	if err := rp.fetch(ctx, c.Fix.SHA, c.Fix.Parent); err != nil {
		return exitDisagree, err
	}
	files, err := rp.diff(ctx, c.Fix.Parent, c.Fix.SHA)
	if err != nil {
		return exitDisagree, err
	}
	b, err := yaml.Marshal(map[string][]bench.FileTruth{"truth": bench.GoTruth(files)})
	if err != nil {
		return exitDisagree, fmt.Errorf("encode truth: %w", err)
	}
	_, err = stdout.Write(b)
	return exitAgrees, err
}
