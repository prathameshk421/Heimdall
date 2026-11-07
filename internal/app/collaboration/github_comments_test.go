package collaboration

import (
	"context"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/stretchr/testify/require"
)

type pollClock struct{ now time.Time }

func (f pollClock) Now() time.Time { return f.now }

type pollCursors struct {
	values map[string]string
	writes int
}

func (f *pollCursors) GetCursor(_ context.Context, key string) (string, error) {
	v, ok := f.values[key]
	if !ok {
		return "", ports.ErrNotFound
	}
	return v, nil
}
func (f *pollCursors) SetCursor(_ context.Context, key, value string) error {
	f.values[key] = value
	f.writes++
	return nil
}

type pollPRs struct{ values []ports.PR }

func (f pollPRs) PR(_ context.Context, _ change.Repo, n int) (ports.PR, error) {
	for _, pr := range f.values {
		if pr.Number == n {
			return pr, nil
		}
	}
	return ports.PR{}, ports.ErrNotFound
}
func (pollPRs) Files(context.Context, change.Repo, int) ([]ports.ChangedFile, error) { return nil, nil }
func (f pollPRs) OpenPRs(context.Context, change.Repo) ([]ports.PR, error)           { return f.values, nil }

type pollComments struct {
	values []ports.IssueComment
	since  time.Time
}

func (f *pollComments) IssueCommentsSince(_ context.Context, _ change.Repo, _ int, since time.Time) ([]ports.IssueComment, error) {
	f.since = since
	return f.values, nil
}

type pollHandler struct {
	ids  []int64
	fail int64
}

func (f *pollHandler) HandleGitHubComment(_ context.Context, _ change.Repo, _ ports.PR, c ports.IssueComment) error {
	f.ids = append(f.ids, c.ID)
	if c.ID == f.fail {
		return context.Canceled
	}
	return nil
}

func TestGitHubCommentPollerUsesOverlapOrdersAndPersistsOnlyAfterCompleteBatch(t *testing.T) {
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	created := now.Add(-time.Hour)
	pr := ports.PR{Repo: "o/r", Number: 9}
	comments := &pollComments{values: []ports.IssueComment{{ID: 3, CreatedAt: created}, {ID: 1, CreatedAt: created}, {ID: 2, CreatedAt: created.Add(time.Minute)}}}
	cursors := &pollCursors{values: map[string]string{"github:comments:o/r:9": now.Add(-20 * time.Minute).Format(time.RFC3339Nano)}}
	handler := &pollHandler{}
	p, err := NewGitHubCommentPoller(CommentPollConfig{Repos: []change.Repo{"o/r"}, PRs: pollPRs{[]ports.PR{pr}}, Comments: comments, Cursors: cursors, Handler: handler, Clock: pollClock{now}})
	require.NoError(t, err)
	res, err := p.PollOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, []int64{1, 3, 2}, handler.ids)
	require.Equal(t, now.Add(-30*time.Minute), comments.since)
	require.Equal(t, 3, res.Comments)
	require.Equal(t, 1, cursors.writes)
	require.Equal(t, created.Add(time.Minute).Format(time.RFC3339Nano), cursors.values["github:comments:o/r:9"])

	handler.ids = nil
	handler.fail = 3
	res, err = p.PollOnce(context.Background())
	require.NoError(t, err, "one PR failure is reported without stopping other repositories")
	require.Len(t, res.Failed, 1)
	require.Equal(t, 1, cursors.writes, "failed batches must replay from the stored cursor")
}
