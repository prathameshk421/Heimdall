package effects

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// CommitTestsDeps are the dependencies of CommitTests.
type CommitTestsDeps struct {
	Blobs    ports.BlobStore
	Branches ports.BranchWriter
	Store    ports.EventStore
	Ingest   Ingest
	IDs      ports.IDs
	Clock    ports.Clock
	Code     ports.CodeReader
}

// CommitTests performs github_commit_tests effects: it commits verified tests to the test branch
// and records tests.committed.
type CommitTests struct {
	deps CommitTestsDeps
}

// NewCommitTests builds the executor, or refuses when a dependency is missing.
func NewCommitTests(d CommitTestsDeps) (CommitTests, error) {
	var missing []string
	if d.Blobs == nil {
		missing = append(missing, "blobs")
	}
	if d.Branches == nil {
		missing = append(missing, "branches")
	}
	if d.Store == nil {
		missing = append(missing, "store")
	}
	if d.Ingest == nil {
		missing = append(missing, "ingest")
	}
	if d.IDs == nil {
		missing = append(missing, "ids")
	}
	if d.Clock == nil {
		missing = append(missing, "clock")
	}
	if len(missing) > 0 {
		return CommitTests{}, fmt.Errorf("github_commit_tests executor: missing %v", missing)
	}
	return CommitTests{deps: d}, nil
}

// Execute performs the commit unless tests.committed is already stored.
func (c CommitTests) Execute(ctx context.Context, fx change.Effect) error {
	if fx.Kind != change.EffectGitHubCommitTests {
		return failf(nil, "commit_tests executor: effect %s has kind %s", fx.Key, fx.Kind)
	}
	p, err := change.DecodeCommitTestsPayload(fx.Payload)
	if err != nil {
		return failf(err, "commit_tests effect %s: payload refused: %v", fx.Key, err)
	}
	ticket, err := ticketFromKey(fx.Key)
	if err != nil {
		return failf(err, "commit_tests effect %s: extract ticket", fx.Key)
	}

	l, err := load(ctx, c.deps.Store, ticket)
	if err != nil {
		return err
	}
	if l.has(change.EventTestsCommitted) {
		return nil
	}

	files, err := c.readFiles(ctx, ticket, p.Files)
	if err != nil {
		return err
	}

	sha, err := c.deps.Branches.CommitFiles(ctx, p.Repo, p.Branch, p.Message, files)
	if err != nil {
		return failf(err, "commit_tests %s: commit files", ticket)
	}

	treeHash := c.treeHash(ctx, p.Repo, sha, p.Dir)
	return c.ingestCommitted(ctx, ticket, l, sha, treeHash)
}

func (c CommitTests) readFiles(ctx context.Context, ticket id.ChangeID, cf []change.CommitFile) ([]ports.File, error) {
	files := make([]ports.File, 0, len(cf))
	for _, f := range cf {
		rc, err := c.deps.Blobs.Get(ctx, f.BlobRef)
		if err != nil {
			return nil, failf(err, "commit_tests %s: get blob %s", ticket, f.BlobRef)
		}
		content, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return nil, failf(err, "commit_tests %s: read blob %s", ticket, f.BlobRef)
		}
		files = append(files, ports.File{Path: f.Path, Content: content})
	}
	return files, nil
}

func (c CommitTests) treeHash(ctx context.Context, repo change.Repo, sha id.SHA, dir string) id.SHA {
	if c.deps.Code != nil {
		if th, err := c.deps.Code.TreeHash(ctx, repo, sha, dir); err == nil && th != "" {
			return th
		}
	}
	return sha
}

func (c CommitTests) ingestCommitted(ctx context.Context, ticket id.ChangeID, l loadedChange, sha, treeHash id.SHA) error {
	cause := findCommitCause(l)
	if cause == nil {
		return failf(nil, "commit_tests %s: no cause event in the change", ticket)
	}

	payload := change.TestsCommitted{SHA: sha, TreeHash: treeHash}
	if err := payload.Validate(); err != nil {
		return failf(err, "commit_tests %s: tests.committed refused", ticket)
	}

	ev, err := newEvent(c.deps.IDs, c.deps.Clock, *cause, change.EventTestsCommitted,
		change.Actor{Kind: change.ActorGitHub, ID: githubActorID}, payload)
	if err != nil {
		return failf(err, "commit_tests %s: build event", ticket)
	}

	if err := c.deps.Ingest.Ingest(ctx, ticket, []change.Event{ev}); err != nil {
		return failf(err, "commit_tests %s: ingest tests.committed", ticket)
	}
	return nil
}

func findCommitCause(l loadedChange) *change.Event {
	if cause := l.first(change.EventTestsApproved); cause != nil {
		return cause
	}
	if cause := l.first(change.EventTestsVerified); cause != nil {
		return cause
	}
	return l.first(change.EventTestsSubmitted)
}

func ticketFromKey(k change.EffectKey) (id.ChangeID, error) {
	parts := strings.Split(string(k), ":")
	if len(parts) < 3 {
		return "", fmt.Errorf("malformed effect key %q", k)
	}
	return id.ParseChangeID(parts[1])
}
