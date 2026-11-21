package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// MaxSummaryBodyBytes bounds one comment body, marker included. GitHub documents 65,536 characters. The exact limit is
// to be confirmed by the S1 spike (ACTION_MODE 2.6), and this constant changes with its result. The bound counts
// bytes, which is never less strict than characters. The renderer keeps its own budget below it (60,000 with a
// truncation line), so the adapter refuses an oversized body and never cuts one.
const MaxSummaryBodyBytes = 65536

// ErrBodyTooLarge means a comment body, with its marker, is longer than MaxSummaryBodyBytes.
var ErrBodyTooLarge = errors.New("github: comment body is longer than GitHub allows")

// sendJSON sends a JSON body with method and decodes the reply into out when out is not nil.
func (x *ActionAPI) sendJSON(ctx context.Context, token, method, path string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("github %s %s: encode body: %w", method, path, err)
	}
	resp, err := x.c.call(ctx, request{method: method, path: path, auth: token, body: body})
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(resp.body, out); err != nil {
		return fmt.Errorf("github %s: decode: %w", path, err)
	}
	return nil
}

// IssueComments returns every comment on a pull request's conversation.
func (x *ActionAPI) IssueComments(ctx context.Context, repo change.Repo, n int) ([]ports.IssueComment, error) {
	return x.IssueCommentsSince(ctx, repo, n, time.Time{})
}

// IssueCommentsSince returns the comments of a pull request's conversation across all pages. A zero since requests
// the full history. GitHub applies since to the update time of a comment, so an edited old comment is returned too.
func (x *ActionAPI) IssueCommentsSince(ctx context.Context, repo change.Repo, n int, since time.Time) ([]ports.IssueComment, error) {
	base, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	token, err := x.tokens.Reader(ctx)
	if err != nil {
		return nil, err
	}
	query := url.Values{"per_page": {perPage}}
	if !since.IsZero() {
		query.Set("since", since.UTC().Format(time.RFC3339))
	}
	pg, err := listAll[commentJSON](ctx, x.c, token, fmt.Sprintf("%s/issues/%d/comments", base, n), query, "")
	if err != nil {
		return nil, err
	}
	out := make([]ports.IssueComment, 0, len(pg.items))
	for _, c := range pg.items {
		out = append(out, ports.IssueComment{
			ID: c.ID, Login: c.User.Login, UserType: c.User.Type, Body: c.Body,
			CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
		})
	}
	return out, nil
}

// prReviewCommentJSON is one inline review comment as GitHub returns it. A null line decodes to 0.
type prReviewCommentJSON struct {
	ID           int64     `json:"id"`
	Body         string    `json:"body"`
	Path         string    `json:"path"`
	Line         int       `json:"line"`
	StartLine    int       `json:"start_line"`
	OriginalLine int       `json:"original_line"`
	Side         string    `json:"side"`
	CommitID     string    `json:"commit_id"`
	InReplyToID  int64     `json:"in_reply_to_id"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	User         struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"user"`
}

// PRReviewComments returns every inline review comment of pull request n across all pages. Bodies are untrusted text.
func (x *ActionAPI) PRReviewComments(ctx context.Context, repo change.Repo, n int) ([]ports.ReviewComment, error) {
	base, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	token, err := x.tokens.Reader(ctx)
	if err != nil {
		return nil, err
	}
	pg, err := listAll[prReviewCommentJSON](ctx, x.c, token, fmt.Sprintf("%s/pulls/%d/comments", base, n), url.Values{"per_page": {perPage}}, "")
	if err != nil {
		return nil, err
	}
	out := make([]ports.ReviewComment, 0, len(pg.items))
	for _, c := range pg.items {
		out = append(out, ports.ReviewComment{
			ID: c.ID, Login: c.User.Login, UserType: c.User.Type, Body: c.Body, Path: c.Path,
			Line: c.Line, StartLine: c.StartLine, OriginalLine: c.OriginalLine, Side: c.Side, CommitID: c.CommitID,
			InReplyTo: c.InReplyToID, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
		})
	}
	return out, nil
}

// Comment writes the summary comment of a pull request. The body carries a hidden marker built from key. A repeat
// edits the comment that has the marker and was written by the configured author, and adds none. A comment of any
// other author that holds the marker is never edited: a person can copy a marker, and the adapter ignores the copy.
// A body longer than MaxSummaryBodyBytes with its marker is ErrBodyTooLarge, and nothing is sent.
func (x *ActionAPI) Comment(ctx context.Context, repo change.Repo, n int, key change.EffectKey, body string) error {
	_, err := x.upsert(ctx, repo, n, key, body)
	return err
}

// UpsertComment is Comment that also returns the id of the comment it created or edited, so the caller can record
// which comment holds the summary. A reply without a positive id is an error: a log entry must point at a comment.
func (x *ActionAPI) UpsertComment(ctx context.Context, repo change.Repo, n int, key change.EffectKey, body string) (int64, error) {
	got, err := x.upsert(ctx, repo, n, key, body)
	if err != nil {
		return 0, err
	}
	if got <= 0 {
		return 0, errors.New("github: the comment reply names no comment id")
	}
	return got, nil
}

// upsert creates or edits the marked comment and returns the id the reply names, or 0 when it names none.
func (x *ActionAPI) upsert(ctx context.Context, repo change.Repo, n int, key change.EffectKey, body string) (int64, error) {
	if err := checkKey(key); err != nil {
		return 0, err
	}
	if x.author == "" {
		return 0, errors.New("github: comment needs the author login of the token")
	}
	base, err := repoPath(repo)
	if err != nil {
		return 0, err
	}
	marker := fmt.Sprintf(effectMarker, key)
	text := body + "\n\n" + marker
	if len(text) > MaxSummaryBodyBytes {
		return 0, ErrBodyTooLarge
	}
	existing, err := x.IssueComments(ctx, repo, n)
	if err != nil {
		return 0, err
	}
	token, err := x.tokens.Writer(ctx, OpCommentWrite, repo)
	if err != nil {
		return 0, err
	}
	method, path := http.MethodPost, fmt.Sprintf("%s/issues/%d/comments", base, n)
	for _, c := range existing {
		if strings.EqualFold(c.Login, x.author) && strings.Contains(c.Body, marker) {
			method, path = http.MethodPatch, fmt.Sprintf("%s/issues/comments/%d", base, c.ID)
			break
		}
	}
	var out struct {
		ID int64 `json:"id"`
	}
	if err := x.sendJSON(ctx, token, method, path, map[string]string{"body": text}, &out); err != nil {
		return 0, err
	}
	return out.ID, nil
}
