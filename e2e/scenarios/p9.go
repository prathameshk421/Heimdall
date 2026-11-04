package scenarios

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/e2e"
)

func p9() e2e.Scenario {
	return e2e.Scenario{ID: "P9", Title: "Set up in minutes", Steps: []e2e.Step{p91(), p92(), p93(), p94()}}
}

// probeValue is a value that stands in for a secret and is not one, used to see whether the command line echoes or
// accepts a secret.
const probeValue = "hd-probe-value-5d2c8e7a"

func p91() e2e.Step {
	return e2e.Step{
		ID: "P9.1",
		// NoStory: the command line and its credentials do not read the story.
		NoStory: true,
		Does:    "`heimdall connect`",
		Sees:    "a table of integrations with status and what each needs; no secret is ever accepted as an argument or echoed",
		Tags:    []string{"local"},
		// cli: the person types at the built command line; nothing else is touched.
		Needs: []e2e.Need{e2e.NeedCLI},
		Run:   runP91,
	}
}

// connectRow is one row of the table `heimdall connect` prints.
type connectRow struct {
	Kind, Status, Needs string
}

var (
	reColumns = regexp.MustCompile(`\s*\|\s*|\s{2,}`)
	reStatus  = regexp.MustCompile(`(?i)\bstatus\b`)
	reNeeds   = regexp.MustCompile(`(?i)\bneeds?\b`)
)

// parseConnect reads the table. The contract: a header line with a "status" column and a "needs" column, then one
// line per integration with its name first, columns separated by "|" or by two or more spaces.
func parseConnect(out string) ([]connectRow, error) {
	var rows []connectRow
	in := false
	for _, line := range strings.Split(out, "\n") {
		cols := reColumns.Split(strings.TrimSpace(strings.Trim(line, "|")), -1)
		switch {
		case !in && len(cols) >= 3 && reStatus.MatchString(line) && reNeeds.MatchString(line):
			in = true
		case in && len(cols) >= 3 && !strings.HasPrefix(strings.TrimSpace(line), "-") && !strings.HasPrefix(strings.TrimSpace(line), "|-"):
			rows = append(rows, connectRow{Kind: strings.ToLower(cols[0]), Status: cols[1], Needs: cols[2]})
		}
	}
	if !in || len(rows) == 0 {
		return nil, &e2e.Red{Expected: "a table of integrations with status and what each needs", Saw: brief(out)}
	}
	return rows, nil
}

func runP91(ctx context.Context, e *e2e.Env) error {
	cli := e.World.Heimdall()
	res, err := cli.RunWithEnv(ctx, []string{"GITHUB_TOKEN=" + probeValue}, "connect")
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return &e2e.Red{Expected: "heimdall connect to succeed", Saw: fmt.Sprintf("exit %d: %s", res.ExitCode, brief(res.Stderr))}
	}
	if _, err := parseConnect(res.Stdout); err != nil {
		return err
	}
	if strings.Contains(res.Stdout+res.Stderr, probeValue) {
		return &e2e.Red{Expected: "a secret in the environment never to be echoed", Saw: "the secret is on the screen"}
	}
	arg, err := cli.Run(ctx, "connect", "--token", probeValue)
	if err != nil {
		return err
	}
	if arg.ExitCode == 0 {
		return &e2e.Red{Expected: "a secret given as an argument to be refused", Saw: "exit 0"}
	}
	if strings.Contains(arg.Stdout+arg.Stderr, probeValue) {
		return &e2e.Red{Expected: "a refused secret not to be echoed", Saw: "the secret is on the screen"}
	}
	return nil
}

func p92() e2e.Step {
	return e2e.Step{
		ID: "P9.2",
		// NoStory: the command line and its credentials do not read the story.
		NoStory: true,
		Does:    "Connect GitHub and Jira with real test credentials, follow the prompts",
		Sees:    "both `connected`; a wrong token gives an error that names the secret file to fix",
		Tags:    []string{"local"},
		// cli: the command line; github-user and tracker: the real test credentials it checks live.
		Needs: []e2e.Need{e2e.NeedCLI, e2e.NeedGitHubUser, e2e.NeedTracker},
		Run:   runP92,
	}
}

var (
	reConnected    = regexp.MustCompile(`(?i)\bconnected\b`)
	reNotConnected = regexp.MustCompile(`(?i)\bnot connected\b|\bfailed\b`)
)

// runP92 contracts the secret files: with `--config <file>` naming a `secrets_dir`, the token for an integration is
// the file `<secrets_dir>/<kind>_token`, and a failed check prints that path.
func runP92(ctx context.Context, e *e2e.Env) (err error) {
	cli := e.World.Heimdall()
	kinds := []string{"github", "jira"}
	for _, kind := range kinds {
		res, err := cli.Run(ctx, "connect", kind)
		if err != nil {
			return err
		}
		if res.ExitCode != 0 || !reConnected.MatchString(res.Stdout) || reNotConnected.MatchString(res.Stdout) {
			return &e2e.Red{Expected: kind + " connected", Saw: fmt.Sprintf("exit %d: %s", res.ExitCode, brief(res.Stdout+res.Stderr))}
		}
	}
	dir, err := os.MkdirTemp("", "heimdall-wrong-token-")
	if err != nil {
		return fmt.Errorf("make a directory for the wrong tokens: %w", err)
	}
	defer func() { err = errors.Join(err, os.RemoveAll(dir)) }()
	for _, kind := range kinds {
		if err := wrongToken(ctx, e, dir, kind); err != nil {
			return err
		}
	}
	return nil
}

// wrongToken connects one integration with a token that is wrong and checks the error names the file to fix.
func wrongToken(ctx context.Context, e *e2e.Env, dir, kind string) error {
	file := filepath.Join(dir, kind+"_token")
	conf := filepath.Join(dir, "heimdall.toml")
	if err := os.WriteFile(file, []byte(probeValue+"\n"), 0o600); err != nil {
		return fmt.Errorf("write the wrong %s token: %w", kind, err)
	}
	if err := os.WriteFile(conf, []byte(fmt.Sprintf("secrets_dir = %q\n", dir)), 0o600); err != nil {
		return fmt.Errorf("write the config: %w", err)
	}
	res, err := e.World.Heimdall().Run(ctx, "connect", kind, "--config", conf)
	if err != nil {
		return err
	}
	out := res.Stdout + res.Stderr
	if res.ExitCode == 0 || !strings.Contains(out, file) {
		return &e2e.Red{Expected: "a wrong " + kind + " token to fail naming " + file, Saw: fmt.Sprintf("exit %d: %s", res.ExitCode, brief(out))}
	}
	if strings.Contains(out, probeValue) {
		return &e2e.Red{Expected: "the wrong token not to be echoed", Saw: "it is on the screen"}
	}
	return nil
}

func p93() e2e.Step {
	return e2e.Step{
		ID: "P9.3",
		// NoStory: the command line and its credentials do not read the story.
		NoStory: true,
		Does:    "Someone who did not write it connects all three systems using only the prompts",
		Sees:    "under 15 minutes, no questions asked of the author (a recorded manual run, T72)",
		Tags:    []string{"manual"},
		// manual: only a person who did not write it can do this; the run is recorded by hand (T72).
		Needs: []e2e.Need{e2e.NeedManual},
		Run:   runP93,
	}
}

func runP93(context.Context, *e2e.Env) error {
	return &e2e.Red{Expected: "a person who did not write it to do this and record the run (T72)", Saw: "the simulator ran it"}
}

func p94() e2e.Step {
	return e2e.Step{
		ID: "P9.4",
		// NoStory: the command line and its credentials do not read the story.
		NoStory: true,
		Does:    "`heimdall connect <kind>` for every other integration, stopped partway and run again",
		Sees:    "each shows its manual steps and resumes where it stopped; a live check proves the result; one without a real run shows `untested` (T99)",
		Tags:    []string{"local"},
		// cli: the command line.
		Needs: []e2e.Need{e2e.NeedCLI},
		Run:   runP94,
	}
}

var (
	reManualStep = regexp.MustCompile(`(?m)^\s*\d+[.)]\s`)
	reResumed    = regexp.MustCompile(`(?i)\b(resum\w*|already done|continu\w*)\b`)
)

// runP94 reads "every other integration" as the table's rows apart from github and jira. "Stopped partway" is what
// happens when the command runs with no one to answer: it lists its manual steps and stops. The second run must
// say it resumed. The contract for the table afterwards: the status is not empty and the needs column says what is
// left.
func runP94(ctx context.Context, e *e2e.Env) error {
	cli := e.World.Heimdall()
	table, err := cli.Run(ctx, "connect")
	if err != nil {
		return err
	}
	rows, err := parseConnect(table.Stdout)
	if err != nil {
		return err
	}
	others := 0
	for _, row := range rows {
		if row.Kind == "github" || row.Kind == "jira" {
			continue
		}
		others++
		first, err := cli.Run(ctx, "connect", row.Kind)
		if err != nil {
			return err
		}
		if !reManualStep.MatchString(first.Stdout) {
			return &e2e.Red{Expected: row.Kind + " to show its manual steps", Saw: brief(first.Stdout)}
		}
		second, err := cli.Run(ctx, "connect", row.Kind)
		if err != nil {
			return err
		}
		if !reResumed.MatchString(second.Stdout) {
			return &e2e.Red{Expected: row.Kind + " to resume where it stopped", Saw: brief(second.Stdout)}
		}
	}
	if others == 0 {
		return &e2e.Red{Expected: "integrations other than github and jira in the table", Saw: fmt.Sprintf("%d rows, all of them those", len(rows))}
	}
	return nil
}
