package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/cardgen"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/pack"
)

const (
	defaultCardsWork  = ".heimdall/cards"
	signalsFileName   = "signals.md"
	scanStateFileName = "scan.json"
	defaultScanDays   = 180
)

// scanState is what scan saves for the prompt command: everything except the signal text, which is in signals.md.
type scanState struct {
	Repo     string
	RepoName string
	Identity string
	Docs     []string
	Sessions []cardgen.Session
	Leads    []cardgen.Lead
	PRLinks  []cardgen.PRLink
}

type scanFlags struct {
	repo, out string
	days      int
}

func parseScanFlags(args []string, env cardsEnv) (scanFlags, bool) {
	fs := flag.NewFlagSet("heimdall cards scan", flag.ContinueOnError)
	fs.SetOutput(env.errw)
	var f scanFlags
	fs.StringVar(&f.repo, "repo", ".", "repository root")
	fs.StringVar(&f.out, "out", "", "work folder for signals.md (default <repo>/.heimdall/cards)")
	fs.IntVar(&f.days, "days", defaultScanDays, "how many days of sessions and history to read")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || f.days < 1 {
		return scanFlags{}, false
	}
	root, err := filepath.Abs(f.repo)
	if err != nil {
		return scanFlags{}, false
	}
	f.repo = root
	if f.out == "" {
		f.out = filepath.Join(root, defaultCardsWork)
	}
	return f, true
}

// runCardsScan reads the developer's sessions and git history on this machine and writes signals.md. Nothing is
// sent anywhere.
func runCardsScan(args []string, env cardsEnv) int {
	f, ok := parseScanFlags(args, env)
	if !ok {
		writeLine(env.out, "usage: heimdall cards scan [--repo .] [--days 180] [--out dir]")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	state, signals, err := scanRepo(ctx, f, env)
	if err != nil {
		writeLine(env.out, "heimdall cards scan: "+err.Error())
		return 1
	}
	if err := writeScanOutput(f.out, state, signals); err != nil {
		writeLine(env.out, "heimdall cards scan: "+err.Error())
		return 1
	}
	writeLine(env.out, fmt.Sprintf("scanned %d session places, found %d signals, %d git leads, %d pull requests",
		len(state.Sessions), len(signals), len(state.Leads), len(state.PRLinks)))
	writeLine(env.out, "wrote "+filepath.Join(f.out, signalsFileName)+" (local only, nothing was sent)")
	writeLine(env.out, "add .heimdall/ to your .gitignore: the signals file holds text from your sessions")
	writeLine(env.out, "next: heimdall cards prompt")
	return 0
}

func scanRepo(ctx context.Context, f scanFlags, env cardsEnv) (scanState, []cardgen.Signal, error) {
	if info, err := os.Stat(f.repo); err != nil || !info.IsDir() {
		return scanState{}, nil, fmt.Errorf("repository %q is not a folder", f.repo)
	}
	redact := func(s string) string { r, _ := pack.RedactReport(s); return r }
	name, email := gitIdentity(ctx, env.git, f.repo)
	state := scanState{Repo: f.repo, RepoName: filepath.Base(f.repo), Identity: strings.TrimSpace(name + " <" + email + ">"), Docs: findDocs(f.repo)}
	cutoff := env.now().AddDate(0, 0, -f.days)
	claude := newestSessions(claudeSessionFiles(env.home, f.repo), cutoff, maxSessionFiles)
	codex := newestSessions(codexSessionFiles(env.home), cutoff, maxSessionFiles)
	files := newestSessions(slices.Concat(claude, codex), cutoff, maxSessionFiles)
	var turns []cardgen.Turn
	used := map[string]int{}
	for _, sf := range files {
		parsed, err := readSession(sf, f.repo)
		if err != nil || len(parsed.Turns) == 0 {
			continue
		}
		used[sf.Agent]++
		turns = append(turns, parsed.Turns...)
		for _, l := range parsed.Links {
			state.PRLinks = addLink(state.PRLinks, l)
		}
	}
	state.Sessions = sessionPlaces(env.home, f.repo, used)
	if email == "" {
		writeLine(env.errw, "git user.email is not set: git leads skipped")
	} else if leads, err := gitLeads(ctx, env.git, f.repo, email, f.days, redact); err != nil {
		writeLine(env.errw, "git leads skipped: "+err.Error())
	} else {
		state.Leads = leads
	}
	return state, cardgen.ExtractSignals(turns, redact), nil
}

// sessionPlaces names the places the agent may read for more than the signals show.
func sessionPlaces(home, root string, used map[string]int) []cardgen.Session {
	var out []cardgen.Session
	if used["claude"] > 0 {
		out = append(out, cardgen.Session{Agent: "claude", Path: filepath.Join(home, ".claude", "projects", encodeProjectPath(root)+"*") + string(filepath.Separator),
			Note: "one .jsonl file per session, one JSON record per line; read the user and assistant text only; the '*' includes worktree folders"})
	}
	if used["codex"] > 0 {
		out = append(out, cardgen.Session{Agent: "codex", Path: filepath.Join(home, ".codex", "sessions"),
			Note: "rollout-*.jsonl files; use only files whose session_meta cwd is inside this repository"})
	}
	db := filepath.Join(home, ".local", "share", "opencode", "opencode.db")
	if _, err := os.Stat(db); err == nil {
		out = append(out, cardgen.Session{Agent: "opencode", Path: db, Note: "SQLite database; open it read-only and use only sessions of this repository"})
	}
	return out
}

func findDocs(root string) []string {
	candidates := []string{"README.md", "CLAUDE.md", "AGENTS.md", "CONTRIBUTING.md", "ARCHITECTURE.md", "CODEOWNERS", ".github/CODEOWNERS"}
	more, _ := filepath.Glob(filepath.Join(root, "docs", "*.md"))
	slices.Sort(more)
	for _, m := range firstN(more, 8) {
		if rel, err := filepath.Rel(root, m); err == nil {
			candidates = append(candidates, filepath.ToSlash(rel))
		}
	}
	var docs []string
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(root, c)); err == nil {
			docs = append(docs, c)
		}
	}
	return docs
}

func writeScanOutput(dir string, state scanState, signals []cardgen.Signal) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, signalsFileName), []byte(renderSignals(state, signals)), 0o600); err != nil {
		return fmt.Errorf("write signals: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode scan state: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, scanStateFileName), data, 0o600); err != nil {
		return fmt.Errorf("write scan state: %w", err)
	}
	return nil
}

func renderSignals(state scanState, signals []cardgen.Signal) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Signals for %s\n\n", state.RepoName)
	b.WriteString("These moments come from your past agent sessions on this machine. Each is a hint. Verify it against the code and history before you write a card. A count above 1 means the same thing came up several times.\n\n")
	b.WriteString("## Signals\n\n")
	if len(signals) == 0 {
		b.WriteString("No signals found.\n\n")
	}
	for i, s := range signals {
		fmt.Fprintf(&b, "### %d. %s (count %d)\n\n%s\n\n- session: %s\n- date: %s\n", i+1, s.Kind, s.Count, s.Text, s.Session, s.Date)
		if len(s.Files) > 0 {
			fmt.Fprintf(&b, "- files: %s\n", strings.Join(s.Files, ", "))
		}
		b.WriteString("\n")
	}
	if len(state.PRLinks) > 0 {
		b.WriteString("## Session to pull request\n\n")
		for _, l := range state.PRLinks {
			fmt.Fprintf(&b, "- session %s: PR #%d %s\n", l.Session, l.Number, l.URL)
		}
		b.WriteString("\n")
	}
	if len(state.Leads) > 0 {
		b.WriteString("## Git leads\n\n")
		for _, l := range state.Leads {
			fmt.Fprintf(&b, "- %s %s: %s (%s)\n", l.Kind, l.Ref, l.Text, l.Detail)
		}
	}
	return b.String()
}
