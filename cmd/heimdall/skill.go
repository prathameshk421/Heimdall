package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/prathameshk421/flip_the_switch_hackathon/skills"
)

// skillName is the folder a coding agent loads the setup skill from.
const skillName = "heimdall-setup"

// runSkill prints the setup skill, or installs it as <dir>/heimdall-setup/SKILL.md for a coding agent.
func runSkill(args []string, stdout, stderr io.Writer, home func() (string, error)) int {
	if len(args) == 0 || args[0] == "print" {
		writeDiagnostic(stdout, skills.SetupSkill)
		return 0
	}
	if args[0] != "install" {
		writeDiagnostic(stderr, "usage: heimdall skill [print | install [-dir DIR]]\n")
		return 2
	}
	fs := flag.NewFlagSet("heimdall skill install", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "the skills folder of your coding agent (default ~/.claude/skills)")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() > 0 {
		return 2
	}
	if *dir == "" {
		h, err := home()
		if err != nil {
			writeDiagnostic(stderr, fmt.Sprintf("heimdall skill install: find the home folder: %v; pass -dir\n", err))
			return 1
		}
		*dir = filepath.Join(h, ".claude", "skills")
	}
	target := filepath.Join(*dir, skillName)
	if err := os.MkdirAll(target, 0o750); err != nil {
		writeDiagnostic(stderr, fmt.Sprintf("heimdall skill install: %v\n", err))
		return 1
	}
	path := filepath.Join(target, "SKILL.md")
	if err := os.WriteFile(path, []byte(skills.SetupSkill), 0o600); err != nil {
		writeDiagnostic(stderr, fmt.Sprintf("heimdall skill install: %v\n", err))
		return 1
	}
	writeDiagnostic(stdout, "installed "+path+"\nAsk your agent: \"Set up Heimdall on OWNER/REPO\".\n")
	return 0
}

// versionString is the commit this binary was built from, marked when the checkout had changes.
func versionString() string {
	if c := buildCommit(); c != "" {
		return c
	}
	return "(built from a checkout with local changes, or without git)"
}
