package gitlog

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// The helpers of the ref readers that hold no MAC key (the monthly budget of ACTION_MODE 7, and the reset of a change
// that failed verification in ACTION_MODE 2.7). They read the ref, its commit, the recursive tree and one blob, so a
// reader can be built without a key.

// containsDotDot reports whether a ref carries "..", which git refuses in a ref name.
func containsDotDot(ref string) bool { return strings.Contains(ref, "..") }

// readTip returns the commit a full ref points at, or "" when the ref does not exist yet.
func readTip(ctx context.Context, git Git, repo change.Repo, ref string) (id.SHA, error) {
	sha, err := git.GetRef(ctx, repo, ref)
	if errors.Is(err, ports.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return sha, nil
}

// listTree reads a whole tree in one recursive listing and indexes it by path. A listing over the limit is a size
// failure, not a missing tree.
func listTree(ctx context.Context, git Git, repo change.Repo, root id.SHA) (tree, error) {
	items, err := git.GetTree(ctx, repo, root, maxTreeEntries)
	if errors.Is(err, ports.ErrObjectTooLarge) {
		return nil, fmt.Errorf("state tree listing: %w: %w", ErrTooLarge, err)
	}
	if err != nil {
		return nil, err
	}
	t := make(tree, len(items))
	for _, it := range items {
		t[it.Path] = it
	}
	return t, nil
}

// readBlob reads one file of at most limit bytes and maps an oversized one to a LimitError.
func readBlob(ctx context.Context, git Git, repo change.Repo, sha id.SHA, what string, limit int64) ([]byte, error) {
	data, err := git.GetBlob(ctx, repo, sha, limit)
	if errors.Is(err, ports.ErrObjectTooLarge) {
		return nil, fmt.Errorf("%w: %w", &LimitError{What: what, Max: limit}, err)
	}
	return data, err
}
