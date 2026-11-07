package agenttools

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// PRFiles lists the changed files of a pull request with their patches.
type PRFiles interface {
	Files(ctx context.Context, repo change.Repo, n int) ([]ports.ChangedFile, error)
}

// prInputs is what a reviewer reads to learn what a pull request changes.
type prInputs struct {
	diff  workflow.BlobRef
	files []string
}

// prKey names one head of one pull request. A push changes the head, so it reads the diff again.
type prKey struct {
	repo   change.Repo
	number int
	head   id.SHA
}

// prInputCache keeps the inputs of each head, because every tool call of an agent rebuilds its assignment.
type prInputCache struct {
	mu    sync.Mutex
	items map[prKey]prInputs
}

func (c *prInputCache) get(k prKey) (prInputs, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.items[k]
	return v, ok
}

func (c *prInputCache) put(k prKey, v prInputs) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.items == nil {
		c.items = make(map[prKey]prInputs)
	}
	c.items[k] = v
}

// latestPR returns the pull request of a change at its newest head. The bool is false before a pull request exists.
func latestPR(events []change.Event) (prKey, bool) {
	var k prKey
	found := false
	for _, ev := range events {
		payload, err := change.Decode(ev)
		if err != nil {
			continue
		}
		switch p := payload.(type) {
		case change.PROpened:
			k = prKey{repo: p.Repo, number: p.Number, head: p.HeadSHA}
			found = true
		case change.PRPushed:
			k = prKey{repo: p.Repo, number: p.Number, head: p.HeadSHA}
			found = true
		}
	}
	return k, found
}

// addPRInputs fills the diff and the changed files of a task that reads a pull request. A test-author works from the
// base and never sees the implementation, so its inputs stay as they are.
func (s *Service) addPRInputs(ctx context.Context, a *Assignment, events []change.Event) error {
	if s.prFiles == nil || a.Role == workflow.RoleTestAuthor {
		return nil
	}
	k, found := latestPR(events)
	if !found {
		return nil
	}
	in, cached := s.prCache.get(k)
	if !cached {
		files, err := s.prFiles.Files(ctx, k.repo, k.number)
		if err != nil {
			return fmt.Errorf("read the files of pull request %s#%d: %w", k.repo, k.number, err)
		}
		ref, err := s.blobs.Put(ctx, ports.BlobDiff, strings.NewReader(renderDiff(files)))
		if err != nil {
			return fmt.Errorf("store the diff of pull request %s#%d: %w", k.repo, k.number, err)
		}
		in = prInputs{diff: ref, files: make([]string, 0, len(files))}
		for _, f := range files {
			in.files = append(in.files, f.Path)
		}
		s.prCache.put(k, in)
	}
	a.Task.Inputs.Diff = in.diff
	a.Task.Inputs.FilesChanged = in.files
	return nil
}

// renderDiff writes each changed file as a header and its patch. GitHub sends no patch for a binary or a very large
// file, and the header says so, because the reader must not take the missing lines for an unchanged file.
func renderDiff(files []ports.ChangedFile) string {
	var b strings.Builder
	for _, f := range files {
		fmt.Fprintf(&b, "=== %s (%s, +%d -%d)\n", f.Path, f.Status, f.Additions, f.Deletions)
		if f.Patch == "" {
			b.WriteString("(GitHub sent no patch for this file. Read it with read_code.)\n\n")
			continue
		}
		b.WriteString(f.Patch)
		b.WriteString("\n\n")
	}
	return b.String()
}
