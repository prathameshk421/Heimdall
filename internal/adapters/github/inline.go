package github

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

type reviewCommentJSON struct {
	Body string `json:"body"`
	User struct {
		Type string `json:"type"`
	} `json:"user"`
}

// InlineComment writes body on one line of a pull request's diff, once per key. GitHub refuses a line outside the diff
// hunks, and a finding may rightly point at one, so such a comment goes on the file instead.
func (a *Adapter) InlineComment(ctx context.Context, repo change.Repo, n int, sha id.SHA, path string, line int, key change.EffectKey, body string) error {
	if err := checkKey(key); err != nil {
		return err
	}
	base, err := repoPath(repo)
	if err != nil {
		return err
	}
	marker := fmt.Sprintf(effectMarker, key)
	reader, err := a.tokens.Reader(ctx)
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/pulls/%d/comments", base, n)
	pg, err := listAll[reviewCommentJSON](ctx, a.c, reader, endpoint, url.Values{"per_page": {"100"}}, "")
	if err != nil {
		return err
	}
	for _, c := range pg.items {
		if c.User.Type == "Bot" && strings.Contains(c.Body, marker) {
			return nil
		}
	}
	token, err := a.tokens.Writer(ctx, OpCommentWrite, repo)
	if err != nil {
		return err
	}
	text := body + "\n\n" + marker
	onLine := map[string]any{"body": text, "commit_id": string(sha), "path": path, "line": line, "side": "RIGHT"}
	if err := a.send(ctx, token, "POST", endpoint, onLine, nil); err == nil {
		return nil
	}
	onFile := map[string]any{"body": text, "commit_id": string(sha), "path": path, "subject_type": "file"}
	return a.send(ctx, token, "POST", endpoint, onFile, nil)
}
