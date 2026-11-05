package github

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

var _ ports.ReviewReader = (*Adapter)(nil)

// PRReviews returns every submitted review of pull request n, mapping states and commit SHAs.
// Pending reviews are skipped, and the gate reads only reviews on the head commit.
func (a *Adapter) PRReviews(ctx context.Context, repo change.Repo, n int) ([]ports.PRReview, error) {
	base, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	token, err := a.tokens.Reader(ctx)
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("%s/pulls/%d/reviews", base, n)
	pg, err := listAll[reviewJSON](ctx, a.c, token, path, url.Values{"per_page": {perPage}}, "")
	if err != nil {
		return nil, err
	}
	out := make([]ports.PRReview, 0, len(pg.items))
	for _, r := range pg.items {
		var state ports.ReviewState
		switch r.State {
		case "APPROVED":
			state = ports.ReviewStateApproved
		case "CHANGES_REQUESTED":
			state = ports.ReviewStateChangesRequested
		case "COMMENTED":
			state = ports.ReviewStateCommented
		case "DISMISSED":
			state = ports.ReviewStateDismissed
		case "PENDING":
			continue
		default:
			return nil, fmt.Errorf("github: review %d: unknown review state %q", r.ID, r.State)
		}

		sha, err := id.ParseSHA(r.CommitID)
		if err != nil {
			return nil, fmt.Errorf("github: review %d: %w", r.ID, err)
		}

		rev := ports.PRReview{
			ID:          strconv.FormatInt(r.ID, 10),
			Login:       change.ActorID(r.User.Login),
			State:       state,
			CommitSHA:   sha,
			SubmittedAt: r.SubmittedAt,
		}
		if err := rev.Validate(); err != nil {
			return nil, fmt.Errorf("github: review %d: %w", r.ID, err)
		}
		out = append(out, rev)
	}
	return out, nil
}
