// Command heimdall-mine reports forgotten companions as SARIF (REVIEW_ARCH 4.6). It is a check in the checks image:
// it reads the base branch history and the change's files, and writes SARIF to stdout.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/mine"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "heimdall-mine:", err)
		os.Exit(2)
	}
}

func run(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("heimdall-mine", flag.ContinueOnError)
	history := fs.String("history", "", "JSON lines of commits, oldest first: {hash, parents, files}")
	change := fs.String("change", "", "JSON object with the change's files: {files: [...]}")
	rulesOnly := fs.Bool("rules", false, "write the rules as JSON instead of the SARIF")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *history == "" || *change == "" {
		return errors.New("-history and -change are required")
	}
	commits, err := readHistory(*history)
	if err != nil {
		return err
	}
	rules := mine.Mine(commits, mine.DefaultLimits())
	if *rulesOnly {
		b, err := mine.RulesJSON(rules)
		if err != nil {
			return err
		}
		_, err = out.Write(b)
		return err
	}
	files, err := readChange(*change)
	if err != nil {
		return err
	}
	b, err := mine.SARIFJSON(mine.Companions(rules, files))
	if err != nil {
		return err
	}
	_, err = out.Write(b)
	return err
}

// readFile reads one named file. The directory is opened as a filesystem root, so the file is named, not built from
// a string path.
func readFile(path string) ([]byte, error) {
	dir, name := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	return fs.ReadFile(os.DirFS(dir), name)
}

func readHistory(path string) ([]mine.Commit, error) {
	b, err := readFile(path)
	if err != nil {
		return nil, err
	}
	var commits []mine.Commit
	dec := json.NewDecoder(bytes.NewReader(b))
	for dec.More() {
		var c mine.Commit
		if err := dec.Decode(&c); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		commits = append(commits, c)
	}
	return commits, nil
}

func readChange(path string) ([]string, error) {
	b, err := readFile(path)
	if err != nil {
		return nil, err
	}
	var c struct {
		Files []string `json:"files"`
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return c.Files, nil
}
