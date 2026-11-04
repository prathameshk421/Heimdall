package ports

import (
	"context"
	"io"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// The VCS port of TECH_SPEC 2.3 is split by consumer, so each interface has at most five methods (CODING_STANDARDS 1).
// The GitHub adapter (task T24) implements all four.

// PRReader reads pull requests. Consumers: the poller (TECH_SPEC 2.1), the slop rule and the gate.
type PRReader interface {
	// PR returns one pull request.
	PR(ctx context.Context, repo change.Repo, n int) (PR, error)
	// Files returns the diff of one pull request.
	Files(ctx context.Context, repo change.Repo, n int) ([]ChangedFile, error)
	// OpenPRs returns the open pull requests of a repository, for the 10 s poll.
	OpenPRs(ctx context.Context, repo change.Repo) ([]PR, error)
}

// PRComments writes comments to pull requests. Consumer: the outbox drainer. Comments carry a hidden marker built from
// the effect key, so a retry edits the comment instead of adding one (TECH_SPEC 2.4).
type PRComments interface {
	Comment(ctx context.Context, repo change.Repo, n int, key change.EffectKey, body string) error
}

// BranchWriter makes test branches and commits. Consumer: the test-author flow (TECH_SPEC 7).
type BranchWriter interface {
	// EnsureBranch creates name at from when it does not exist, and does nothing when it does.
	EnsureBranch(ctx context.Context, repo change.Repo, from id.SHA, name string) error
	// CommitFiles writes files to branch in one commit and returns its SHA.
	CommitFiles(ctx context.Context, repo change.Repo, branch, msg string, files []File) (id.SHA, error)
}

// CheckWriter publishes check runs. Consumer: the gate (TECH_SPEC 12.4).
type CheckWriter interface {
	// SetCheck creates or updates the check run named by c.ExternalID on sha.
	SetCheck(ctx context.Context, repo change.Repo, sha id.SHA, c Check) error
}

// CodeReader reads code at a commit. Consumers: the test pin (tree hashes, TECH_SPEC 12.4) and the sandbox source.
type CodeReader interface {
	// TreeHash returns the git tree hash of dir at sha.
	TreeHash(ctx context.Context, repo change.Repo, sha id.SHA, dir string) (id.SHA, error)
	// Tarball returns the source tree at sha. The caller closes it.
	Tarball(ctx context.Context, repo change.Repo, sha id.SHA) (io.ReadCloser, error)
}
