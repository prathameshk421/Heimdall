package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// cardsEnv holds what the cards commands take from the machine, so tests can replace it.
type cardsEnv struct {
	stdin    io.Reader
	out      io.Writer
	errw     io.Writer
	git      gitRunner
	home     string
	now      func() time.Time
	client   *http.Client
	lookPath func(string) (string, error)
}

func writeLine(w io.Writer, line string) { _, _ = io.WriteString(w, line+"\n") }

// runCards runs `heimdall cards <scan|prompt|check|push|pull|inbox|accept|reject>`.
func runCards(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	home, err := os.UserHomeDir()
	if err != nil {
		writeLine(stderr, "heimdall cards: no home folder: "+err.Error())
		return 1
	}
	env := cardsEnv{stdin: stdin, out: stdout, errw: stderr, git: execGit, home: home, now: time.Now,
		client: &http.Client{Timeout: 30 * time.Second}, lookPath: exec.LookPath}
	return runCardsWith(args, env)
}

func runCardsWith(args []string, env cardsEnv) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		writeCardsHelp(env.out)
		return 0
	}
	commands := map[string]func([]string, cardsEnv) int{
		"scan":    runCardsScan,
		"prompt":  runCardsPrompt,
		"check":   runCardsCheck,
		"preview": runCardsPreview,
		"push":    runCardsPush,
		"pull":    runCardsPull,
		"inbox":   runCardsInbox,
		"accept":  func(rest []string, env cardsEnv) int { return runCardsSetStatus(rest, env, "active") },
		"reject":  func(rest []string, env cardsEnv) int { return runCardsSetStatus(rest, env, "rejected") },
	}
	run, ok := commands[args[0]]
	if !ok {
		writeLine(env.out, fmt.Sprintf("unknown cards command %q", args[0]))
		return 2
	}
	return run(args[1:], env)
}

func writeCardsHelp(out io.Writer) {
	for _, line := range []string{
		"heimdall cards scan [--repo .] [--days 180] [--out dir]   read your sessions and git on this machine, write signals.md",
		"heimdall cards prompt [--person P] [--interview]          write PROMPT.md and print the command for each agent on PATH",
		"heimdall cards check [--dir dir]                          check every card: format, secrets, vague cards",
		"heimdall cards preview --base d --head d --diff f [--cards d --person P]  print what a reviewer would receive",
		"heimdall cards push --server URL --token-file F [--yes]   check, list and upload your cards",
		"heimdall cards pull --server URL --token-file F           download your cards into the folder",
		"heimdall cards inbox --server URL --token-file F          list cards the learning loop proposed",
		"heimdall cards accept NAME --server URL --token-file F    make a proposed card active",
		"heimdall cards reject NAME --server URL --token-file F    mark a proposed card rejected",
		"Nothing leaves this machine until push, accept or reject. Run scan and prompt, then your own agent writes the cards.",
	} {
		writeLine(out, line)
	}
}

// runCardsPrompt writes PROMPT.md and prints the command that starts each agent found on PATH. It runs nothing.
func runCardsPrompt(args []string, env cardsEnv) int {
	fs := flag.NewFlagSet("heimdall cards prompt", flag.ContinueOnError)
	fs.SetOutput(env.errw)
	person := fs.String("person", "", "name of the person the cards are for (default: git user.name)")
	interview := fs.Bool("interview", false, "let the agent ask you up to 5 questions")
	work := fs.String("out", defaultCardsWork, "work folder written by scan")
	dir := fs.String("dir", "", "folder the agent writes cards into (default <out>/files)")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		writeLine(env.out, "usage: heimdall cards prompt [--person P] [--interview] [--out dir] [--dir dir]")
		return 2
	}
	if *dir == "" {
		*dir = filepath.Join(*work, "files")
	}
	path, err := writePrompt(promptRequest{work: *work, dir: *dir, person: *person, interview: *interview}, env)
	if err != nil {
		writeLine(env.out, "heimdall cards prompt: "+err.Error())
		return 1
	}
	writeLine(env.out, "wrote "+path)
	printAgentCommands(env, path)
	return 0
}

type promptRequest struct {
	work, dir, person string
	interview         bool
}

func writePrompt(req promptRequest, env cardsEnv) (string, error) {
	state, err := readScanState(filepath.Join(req.work, scanStateFileName))
	if err != nil {
		return "", err
	}
	person := req.person
	if person == "" {
		name, _ := gitIdentity(context.Background(), env.git, state.Repo)
		person = strings.ToLower(strings.Join(strings.Fields(name), "-"))
	}
	absDir, err := filepath.Abs(req.dir)
	if err != nil {
		return "", fmt.Errorf("resolve cards folder: %w", err)
	}
	if err := os.MkdirAll(absDir, 0o750); err != nil {
		return "", fmt.Errorf("create cards folder: %w", err)
	}
	text, err := buildPromptText(state, req, person, absDir)
	if err != nil {
		return "", err
	}
	path := filepath.Join(req.work, "PROMPT.md")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		return "", fmt.Errorf("write prompt: %w", err)
	}
	return path, nil
}

func printAgentCommands(env cardsEnv, promptPath string) {
	agents := []struct{ bin, form string }{
		{"claude", `claude "$(cat %s)"`},
		{"codex", `codex "$(cat %s)"`},
		{"opencode", `opencode run "$(cat %s)"`},
	}
	found := false
	for _, a := range agents {
		if _, err := env.lookPath(a.bin); err == nil {
			if !found {
				writeLine(env.out, "run one of these in the repository. Heimdall does not run them for you:")
			}
			found = true
			writeLine(env.out, "  "+fmt.Sprintf(a.form, promptPath))
		}
	}
	if !found {
		writeLine(env.out, "no coding agent (claude, codex, opencode) found on PATH: paste the prompt file into your agent")
	}
	writeLine(env.out, "then: heimdall cards check, and heimdall cards push --server URL --token-file F")
}

// runCardsCheck checks every card in the folder and exits 1 on a problem. Warnings do not fail.
func runCardsCheck(args []string, env cardsEnv) int {
	fs := flag.NewFlagSet("heimdall cards check", flag.ContinueOnError)
	fs.SetOutput(env.errw)
	dir := fs.String("dir", filepath.Join(defaultCardsWork, "files"), "folder with the card files")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		writeLine(env.out, "usage: heimdall cards check [--dir dir]")
		return 2
	}
	names, err := cardFileNames(*dir)
	if err != nil {
		writeLine(env.out, "heimdall cards check: "+err.Error())
		return 1
	}
	if len(names) == 0 {
		writeLine(env.out, "no cards in "+*dir)
		return 1
	}
	if _, bad := checkCards(*dir, names, env.out); bad {
		return 1
	}
	return 0
}

// readInDir reads a file by name inside dir. The name cannot leave dir, not even through a symlink.
func readInDir(dir, name string) ([]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open folder: %w", err)
	}
	defer func() { _ = root.Close() }()
	data, err := root.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	return data, nil
}

// checkCards prints the result per file. It returns the names that passed and whether any file had a problem.
func checkCards(dir string, names []string, out io.Writer) (good []string, bad bool) {
	for _, name := range names {
		data, err := readInDir(dir, name)
		var report cardReport
		if err != nil {
			report.problems = []string{"cannot read: " + err.Error()}
		} else {
			report = checkCardText(name, string(data))
		}
		switch {
		case len(report.problems) > 0:
			bad = true
			writeLine(out, "FAIL "+name)
		case len(report.warnings) > 0:
			good = append(good, name)
			writeLine(out, "warn "+name)
		default:
			good = append(good, name)
			writeLine(out, "ok   "+name)
		}
		for _, p := range report.problems {
			writeLine(out, "     problem: "+p)
		}
		for _, w := range report.warnings {
			writeLine(out, "     warning: "+w)
		}
	}
	return good, bad
}

// cardFileNames lists the card files directly in dir, sorted.
func cardFileNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	var names []string
	for _, e := range entries {
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".md", ".yaml", ".yml", ".txt":
			if e.Type().IsRegular() && !strings.HasPrefix(e.Name(), ".") {
				names = append(names, e.Name())
			}
		}
	}
	slices.Sort(names)
	return names, nil
}
