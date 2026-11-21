package ports

import (
	"context"
	"errors"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// The ports the git-ref event store needs beyond GitRefs and GitObjects (ACTION_MODE 2.3 and 2.4, work package AM8,
// spike S1 in ACTION_MODE_SPIKE.md 1.7). Reads go over REST because GraphQL cannot see refs/heimdall/*. The one write
// that moves the ref is a compare-and-swap through GraphQL updateRefs, because REST PATCH /git/refs is a plain set on
// that namespace.

// ErrObjectTooLarge means an object read from a remote system is larger than the limit the caller gave. The reader
// stops before it decodes the object.
var ErrObjectTooLarge = errors.New("object larger than the limit")

// ErrRefSwapUnconfirmed means a compare-and-swap of a ref came back with an error that does not say whether the ref
// moved. GitHub answers a refused updateRefs with an untyped "Something went wrong" (S1 1.2), and a lost reply looks
// the same. The caller reads the ref again to learn the outcome.
var ErrRefSwapUnconfirmed = errors.New("ref swap outcome unknown")

// GitCommit is the part of a git commit the store reads.
type GitCommit struct {
	Tree    id.SHA
	Parents []id.SHA
}

// GitTreeItem is one entry of a recursive tree listing. Type is blob, tree or commit (a submodule). Path is relative
// and clean.
type GitTreeItem struct {
	Path string
	Mode string
	Type string
	SHA  id.SHA
}

// GitReader reads git objects by hash. Every object is addressed by its hash, so a set of reads that starts from one
// commit sees one consistent state even while others move the ref. Consumer: the git-ref event store (AM8).
type GitReader interface {
	// GetCommit returns the tree and parents of a commit. A missing commit is ErrNotFound.
	GetCommit(ctx context.Context, repo change.Repo, sha id.SHA) (GitCommit, error)
	// GetTree lists a tree and all its subtrees in one call. A listing GitHub marks truncated, or one with more than
	// maxEntries entries, is ErrObjectTooLarge.
	GetTree(ctx context.Context, repo change.Repo, sha id.SHA, maxEntries int) ([]GitTreeItem, error)
	// GetBlob returns the content of a blob. A blob whose size is over maxBytes is ErrObjectTooLarge, checked on the
	// size GitHub states before anything is decoded. A content that does not match the stated size is an error.
	GetBlob(ctx context.Context, repo change.Repo, sha id.SHA, maxBytes int64) ([]byte, error)
}

// GitRefSwapper moves a ref with a compare-and-swap. Consumer: the git-ref event store (AM8).
type GitRefSwapper interface {
	// SwapRef moves ref from before to after, and only if the ref is at before and after is a fast-forward of it.
	// It never forces. nil means the ref moved. ErrUnauthorized means the token may not write the repository's
	// contents: the caller fails closed. Any other error (ErrRefSwapUnconfirmed, ErrUnavailable, or an untyped one)
	// leaves the outcome unknown, and the caller reads the ref to learn it.
	SwapRef(ctx context.Context, repo change.Repo, ref string, before, after id.SHA) error
}
