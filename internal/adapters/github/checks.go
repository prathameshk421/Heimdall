package github

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// maxCommentBytes is GitHub's limit on a comment body.
const maxCommentBytes = 65536

// effectMarker is the hidden line that ties a comment to the effect that wrote it.
const effectMarker = "<!-- heimdall:effect=%s -->"

func encodeBase64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// SetCheck creates the check run named by c.ExternalID on sha, or updates it when one with the same name and external
// id is already there, so a retry never adds a second run.
func (a *Adapter) SetCheck(ctx context.Context, repo change.Repo, sha id.SHA, c ports.Check) error {
	base, err := repoPath(repo)
	if err != nil {
		return err
	}
	if err := checkSHA(sha); err != nil {
		return err
	}
	if err := c.Validate(); err != nil {
		return fmt.Errorf("github: check: %w", err)
	}
	token, err := a.tokens.Writer(ctx, OpCheckWrite, repo)
	if err != nil {
		return err
	}
	existing, err := a.findCheck(ctx, token, base, sha, c)
	if err != nil {
		return err
	}
	body := checkBody{
		Status:     string(c.Status),
		Conclusion: string(c.Conclusion),
		ExternalID: c.ExternalID,
		Output:     checkOutput{Title: c.Name, Summary: c.Summary},
	}
	if existing != 0 {
		return a.send(ctx, token, http.MethodPatch, fmt.Sprintf("%s/check-runs/%d", base, existing), body, nil)
	}
	body.Name = c.Name
	body.HeadSHA = string(sha)
	return a.send(ctx, token, http.MethodPost, base+"/check-runs", body, nil)
}

// findCheck returns the id of the run on sha with c's name and external id, or 0 when there is none.
func (a *Adapter) findCheck(ctx context.Context, token, base string, sha id.SHA, c ports.Check) (int64, error) {
	path := fmt.Sprintf("%s/commits/%s/check-runs", base, sha)
	resp, err := a.get(ctx, token, path, url.Values{"check_name": {c.Name}, "per_page": {perPage}})
	if err != nil {
		return 0, err
	}
	runs, err := decode[checkRunsJSON](resp, path)
	if err != nil {
		return 0, err
	}
	for _, r := range runs.CheckRuns {
		if r.ExternalID == c.ExternalID {
			return r.ID, nil
		}
	}
	return 0, nil
}

// checkKey refuses an effect key that could break out of the hidden marker.
func checkKey(key change.EffectKey) error {
	k := string(key)
	if k == "" || len(k) > 200 || strings.Contains(k, "-->") || strings.ContainsAny(k, "\r\n\x00") {
		return fmt.Errorf("github: effect key %q cannot be put in a comment marker", k)
	}
	return nil
}

// Comment writes body to a pull request. It carries a hidden marker built from key, and a retry edits the comment
// that already has the marker instead of adding another. Only a comment written by a Bot counts, so a person who
// quotes a marker cannot make the adapter edit their text.
func (a *Adapter) Comment(ctx context.Context, repo change.Repo, n int, key change.EffectKey, body string) error {
	if err := checkKey(key); err != nil {
		return err
	}
	base, err := repoPath(repo)
	if err != nil {
		return err
	}
	marker := fmt.Sprintf(effectMarker, key)
	text := body + "\n\n" + marker
	if len(text) > maxCommentBytes {
		return errors.New("github: comment is longer than GitHub allows")
	}
	existing, err := a.IssueComments(ctx, repo, n)
	if err != nil {
		return err
	}
	token, err := a.tokens.Writer(ctx, OpCommentWrite, repo)
	if err != nil {
		return err
	}
	for _, c := range existing {
		if c.UserType == "Bot" && strings.Contains(c.Body, marker) {
			return a.send(ctx, token, http.MethodPatch, fmt.Sprintf("%s/issues/comments/%d", base, c.ID), map[string]string{"body": text}, nil)
		}
	}
	return a.send(ctx, token, http.MethodPost, fmt.Sprintf("%s/issues/%d/comments", base, n), map[string]string{"body": text}, nil)
}
