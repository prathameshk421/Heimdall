package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// perPage is the page size of every list: GitHub's maximum, so a list is as few calls as it can be.
const perPage = "100"

// get sends one authenticated GET and returns the reply.
func (a *Adapter) get(ctx context.Context, token, path string, query url.Values) (*response, error) {
	return a.c.call(ctx, request{method: http.MethodGet, path: path, query: query, auth: token})
}

// send sends a JSON body with method and decodes the reply into out when out is not nil.
func (a *Adapter) send(ctx context.Context, token, method, path string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("github %s %s: encode body: %w", method, path, err)
	}
	resp, err := a.c.call(ctx, request{method: method, path: path, auth: token, body: body})
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

// PR returns one pull request.
func (a *Adapter) PR(ctx context.Context, repo change.Repo, n int) (ports.PR, error) {
	base, err := repoPath(repo)
	if err != nil {
		return ports.PR{}, err
	}
	token, err := a.tokens.Reader(ctx)
	if err != nil {
		return ports.PR{}, err
	}
	path := fmt.Sprintf("%s/pulls/%d", base, n)
	resp, err := a.get(ctx, token, path, nil)
	if err != nil {
		return ports.PR{}, err
	}
	p, err := decode[prJSON](resp, path)
	if err != nil {
		return ports.PR{}, err
	}
	return p.toPR(repo)
}

// Files returns the diff of one pull request, every page of it.
func (a *Adapter) Files(ctx context.Context, repo change.Repo, n int) ([]ports.ChangedFile, error) {
	base, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	token, err := a.tokens.Reader(ctx)
	if err != nil {
		return nil, err
	}
	pg, err := listAll[fileJSON](ctx, a.c, token, fmt.Sprintf("%s/pulls/%d/files", base, n), url.Values{"per_page": {perPage}}, "")
	if err != nil {
		return nil, err
	}
	files := make([]ports.ChangedFile, 0, len(pg.items))
	for _, f := range pg.items {
		cf, err := f.toFile()
		if err != nil {
			return nil, err
		}
		files = append(files, cf)
	}
	return files, nil
}

// OpenPRs returns the open pull requests for the poll. A list that fits one page is cached with its ETag, and a 304
// answers from the cache, which costs no rate limit.
func (a *Adapter) OpenPRs(ctx context.Context, repo change.Repo) ([]ports.PR, error) {
	base, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	token, err := a.tokens.Reader(ctx)
	if err != nil {
		return nil, err
	}
	key := repo.String()
	cached, have := a.c.getPRs(key)
	etag := ""
	if have {
		etag = cached.etag
	}
	query := url.Values{"state": {"open"}, "per_page": {perPage}}
	pg, err := listAll[prJSON](ctx, a.c, token, base+"/pulls", query, etag)
	if err != nil {
		return nil, err
	}
	if pg.notModified {
		return cached.prs, nil
	}
	prs := make([]ports.PR, 0, len(pg.items))
	for _, p := range pg.items {
		pr, err := p.toPR(repo)
		if err != nil {
			return nil, err
		}
		prs = append(prs, pr)
	}
	if pg.etag != "" {
		a.c.putPRs(key, cachedPRs{etag: pg.etag, prs: prs})
	}
	return prs, nil
}

// Reviews returns every submitted review of a pull request. The gate counts an approval only from a login that is not
// the author and only on the head commit, so each review carries both.
func (a *Adapter) Reviews(ctx context.Context, repo change.Repo, n int) ([]Review, error) {
	base, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	token, err := a.tokens.Reader(ctx)
	if err != nil {
		return nil, err
	}
	pg, err := listAll[reviewJSON](ctx, a.c, token, fmt.Sprintf("%s/pulls/%d/reviews", base, n), url.Values{"per_page": {perPage}}, "")
	if err != nil {
		return nil, err
	}
	out := make([]Review, 0, len(pg.items))
	for _, r := range pg.items {
		out = append(out, Review{ID: r.ID, Login: r.User.Login, State: r.State, CommitID: id.SHA(r.CommitID)})
	}
	return out, nil
}

// IssueComments returns every comment on a pull request's conversation.
func (a *Adapter) IssueComments(ctx context.Context, repo change.Repo, n int) ([]IssueComment, error) {
	return a.IssueCommentsSince(ctx, repo, n, time.Time{})
}

// IssueCommentsSince returns comments created at or after since. A zero cursor requests the full history.
func (a *Adapter) IssueCommentsSince(ctx context.Context, repo change.Repo, n int, since time.Time) ([]IssueComment, error) {
	base, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	token, err := a.tokens.Reader(ctx)
	if err != nil {
		return nil, err
	}
	query := url.Values{"per_page": {perPage}}
	if !since.IsZero() {
		query.Set("since", since.UTC().Format(time.RFC3339))
	}
	pg, err := listAll[commentJSON](ctx, a.c, token, fmt.Sprintf("%s/issues/%d/comments", base, n), query, "")
	if err != nil {
		return nil, err
	}
	out := make([]IssueComment, 0, len(pg.items))
	for _, c := range pg.items {
		out = append(out, IssueComment{
			ID: c.ID, Login: c.User.Login, UserType: c.User.Type, Body: c.Body,
			CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
		})
	}
	return out, nil
}
