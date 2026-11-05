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
	// ReadFile returns the content of one file at sha, or ErrNotFound when the path does not exist there. The slop rule
	// reads heimdall.yaml with it at the merge base, never at the PR head (TECH_SPEC invariant 5).
	ReadFile(ctx context.Context, repo change.Repo, sha id.SHA, path string) ([]byte, error)
	// Tarball returns the source tree at sha. The caller closes it.
	Tarball(ctx context.Context, repo change.Repo, sha id.SHA) (io.ReadCloser, error)
}

// RepoReader reads the state of a repository's default branch. Consumer: the branch effect (internal/app/effects), which
// needs the base commit before it makes a test branch. It returns ErrNotFound for an empty repository, which has no
// commit to read.
type RepoReader interface {
	// DefaultHead returns the head commit SHA of the repository's default branch.
	DefaultHead(ctx context.Context, repo change.Repo) (id.SHA, error)
}

// ReviewReader reads reviews of pull requests. Consumer: the gate (TECH_SPEC 12.4).
type ReviewReader interface {
	// PRReviews returns every submitted review of pull request n.
	PRReviews(ctx context.Context, repo change.Repo, n int) ([]PRReview, error)
}

// CheckReader reads check runs on commits. Consumer: the gate (TECH_SPEC 12.4).
type CheckReader interface {
	// CheckRuns returns the check runs on sha, excluding heimdall/gate.
	CheckRuns(ctx context.Context, repo change.Repo, sha id.SHA) ([]CheckRun, error)
}
