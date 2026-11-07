package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/cardsfs"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/codegraph"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/reviewbrief"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// previewGraph answers the compiler from two local folders, with no server and no model.
type previewGraph struct{ head, base *codegraph.Index }

type previewImpact struct {
	codegraph.ImpactMap
	head *codegraph.Index
}

func (v previewImpact) Defined(symbol string) bool { return len(v.head.Definitions(symbol)) > 0 }
func (v previewImpact) Partial() bool              { return v.head.Truncated() }
func (v previewImpact) BaseMissing() bool          { return false }

func (g previewGraph) Impact(_ context.Context, _ change.Repo, _, _ id.SHA, diff string, files []string) (reviewbrief.Impact, error) {
	return previewImpact{ImpactMap: codegraph.Impact(g.head, g.base, diff, files), head: g.head}, nil
}

// runCardsPreview prints what a reviewer would receive for a diff: the rules that wake and the change map. A developer
// uses it to test a card before pushing it, and the review bench uses it to build the briefs it compares.
func runCardsPreview(args []string, env cardsEnv) int {
	flags := flag.NewFlagSet("heimdall cards preview", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	base := flags.String("base", "", "folder that holds the code before the change")
	head := flags.String("head", "", "folder that holds the code after the change")
	diffPath := flags.String("diff", "", "unified diff from base to head")
	cards := flags.String("cards", "", "cards folder with one folder per person; omit to see only the change map")
	person := flags.String("person", "", "the person whose cards to use")
	if err := flags.Parse(args); err != nil || *base == "" || *head == "" || *diffPath == "" {
		writeLine(env.out, "usage: heimdall cards preview --base dir --head dir --diff file [--cards dir --person P]")
		return 2
	}
	text, err := previewBrief(*base, *head, *diffPath, *cards, *person)
	if err != nil {
		writeLine(env.out, "heimdall cards preview: "+err.Error())
		return 1
	}
	writeLine(env.out, text)
	return 0
}

func previewBrief(base, head, diffPath, cardsDir, person string) (string, error) {
	diff, err := os.ReadFile(diffPath) // #nosec G304 -- the developer names this local file on their own machine
	if err != nil {
		return "", fmt.Errorf("read diff: %w", err)
	}
	baseFiles, err := readTree(base)
	if err != nil {
		return "", err
	}
	headFiles, err := readTree(head)
	if err != nil {
		return "", err
	}
	cfg := reviewbrief.Config{
		Logger: slog.New(slog.DiscardHandler),
		Graph:  previewGraph{head: codegraph.FromFiles(headFiles), base: codegraph.FromFiles(baseFiles)},
	}
	if cardsDir != "" {
		store, err := cardsfs.Open(cardsDir)
		if err != nil {
			return "", fmt.Errorf("open cards: %w", err)
		}
		defer func() { _ = store.Close() }()
		cfg.Cards = store
	}
	compiler, err := reviewbrief.New(cfg)
	if err != nil {
		return "", fmt.Errorf("build compiler: %w", err)
	}
	var task workflow.TaskEnvelope
	task.Role = workflow.RoleReviewer
	task.Inputs.FilesChanged = diffFiles(string(diff))
	brief := compiler.Compile(context.Background(), reviewbrief.Request{Person: person, Agent: "preview", Task: task, Diff: string(diff)})
	return fmt.Sprintf("=== personal_context ===\n%s\n\n=== change_map ===\n%s", brief.Obligations, brief.ChangeMap), nil
}

// diffFiles lists the new-side paths of a unified diff.
func diffFiles(diff string) []string {
	var files []string
	for _, line := range strings.Split(diff, "\n") {
		if rest, ok := strings.CutPrefix(line, "+++ "); ok {
			name, _, _ := strings.Cut(rest, "\t")
			if name = strings.TrimSpace(name); name != "/dev/null" {
				files = append(files, strings.TrimPrefix(name, "b/"))
			}
		}
	}
	return files
}

// readTree reads the regular files of a folder through os.Root, so a link cannot lead outside it.
func readTree(dir string) (map[string]string, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", dir, err)
	}
	defer func() { _ = root.Close() }()
	files := map[string]string{}
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		content, err := root.ReadFile(name)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(name)] = string(content)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	return files, nil
}
