package collaboration

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const githubCommentOverlap = 10 * time.Minute

// GitHubCommentHandler handles a comment after it has been loaded for an open PR. Implementations resolve the PR to
// its change and pass CreatedAt != UpdatedAt and UserType != "User" to Processor.Handle.
type GitHubCommentHandler interface {
	HandleGitHubComment(ctx context.Context, repo change.Repo, pr ports.PR, comment ports.IssueComment) error
}

// CommentPollConfig contains one polling cycle's dependencies.
type CommentPollConfig struct {
	Repos    []change.Repo
	PRs      ports.PRReader
	Comments ports.PRCommentReader
	Cursors  ports.CursorTable
	Handler  GitHubCommentHandler
	Clock    ports.Clock
}

// GitHubCommentPoller polls issue comments on open PRs and persists overlapping cursors.
type GitHubCommentPoller struct {
	repos    []change.Repo
	prs      ports.PRReader
	comments ports.PRCommentReader
	cursors  ports.CursorTable
	handler  GitHubCommentHandler
	clock    ports.Clock
}

// CommentPollResult counts processed and failed pull requests.
type CommentPollResult struct {
	Comments int
	Failed   []CommentPollFailure
}

// CommentPollFailure identifies a PR whose comments did not finish processing.
type CommentPollFailure struct {
	Repo   change.Repo
	Number int
	Err    error
}

// NewGitHubCommentPoller validates dependencies and builds the poller.
func NewGitHubCommentPoller(cfg CommentPollConfig) (*GitHubCommentPoller, error) {
	var missing []string
	if len(cfg.Repos) == 0 {
		missing = append(missing, "Repos")
	}
	if cfg.PRs == nil {
		missing = append(missing, "PRs")
	}
	if cfg.Comments == nil {
		missing = append(missing, "Comments")
	}
	if cfg.Cursors == nil {
		missing = append(missing, "Cursors")
	}
	if cfg.Handler == nil {
		missing = append(missing, "Handler")
	}
	if cfg.Clock == nil {
		missing = append(missing, "Clock")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("github comment poller: missing %v", missing)
	}
	return &GitHubCommentPoller{
		repos: slices.Clone(cfg.Repos), prs: cfg.PRs, comments: cfg.Comments, cursors: cfg.Cursors,
		handler: cfg.Handler, clock: cfg.Clock,
	}, nil
}

// PollOnce lists comments for each open PR. It starts with a ten-minute overlap, then re-queries that overlap on each
// poll to cover GitHub's indexing lag. Duplicate comments are expected and the command action must deduplicate by ID.
func (p *GitHubCommentPoller) PollOnce(ctx context.Context) (CommentPollResult, error) {
	var result CommentPollResult
	for _, repo := range p.repos {
		prs, err := p.prs.OpenPRs(ctx, repo)
		if err != nil {
			return result, fmt.Errorf("list PRs in %s for comment polling: %w", repo, err)
		}
		for _, pr := range prs {
			if err := p.pollPR(ctx, repo, pr, &result); err != nil {
				result.Failed = append(result.Failed, CommentPollFailure{Repo: repo, Number: pr.Number, Err: err})
			}
		}
	}
	return result, nil
}

func (p *GitHubCommentPoller) pollPR(ctx context.Context, repo change.Repo, pr ports.PR, result *CommentPollResult) error {
	key := commentCursorKey(repo, pr.Number)
	now := p.clock.Now().UTC()
	cursor, err := p.loadCommentCursor(ctx, key, now)
	if err != nil {
		return err
	}
	comments, err := p.comments.IssueCommentsSince(ctx, repo, pr.Number, cursor)
	if err != nil {
		return fmt.Errorf("read comments: %w", err)
	}
	slices.SortFunc(comments, compareComments)
	for _, comment := range comments {
		if err := p.handler.HandleGitHubComment(ctx, repo, pr, comment); err != nil {
			return fmt.Errorf("handle comment %d: %w", comment.ID, err)
		}
		result.Comments++
	}
	next := nextCommentCursor(comments, now)
	if err := p.cursors.SetCursor(ctx, key, next); err != nil {
		return fmt.Errorf("save cursor: %w", err)
	}
	return nil
}

func (p *GitHubCommentPoller) loadCommentCursor(ctx context.Context, key string, now time.Time) (time.Time, error) {
	raw, err := p.cursors.GetCursor(ctx, key)
	if errors.Is(err, ports.ErrNotFound) {
		return now.Add(-githubCommentOverlap), nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("load cursor: %w", err)
	}
	cursor, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse cursor: %w", err)
	}
	return cursor.Add(-githubCommentOverlap), nil
}

func compareComments(a, b ports.IssueComment) int {
	if a.CreatedAt.Before(b.CreatedAt) {
		return -1
	}
	if a.CreatedAt.After(b.CreatedAt) {
		return 1
	}
	return cmpInt64(a.ID, b.ID)
}

func nextCommentCursor(comments []ports.IssueComment, now time.Time) string {
	if len(comments) == 0 {
		return now.Format(time.RFC3339Nano)
	}
	last := comments[len(comments)-1].CreatedAt.UTC()
	if last.After(now) {
		last = now
	}
	return last.Format(time.RFC3339Nano)
}

func commentCursorKey(repo change.Repo, number int) string {
	return fmt.Sprintf("github:comments:%s:%d", repo, number)
}

func cmpInt64(a, b int64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
