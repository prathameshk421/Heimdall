package github

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
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

var _ ports.CheckReader = (*Adapter)(nil)

// CheckRuns returns the check runs on sha, excluding heimdall/gate.
func (a *Adapter) CheckRuns(ctx context.Context, repo change.Repo, sha id.SHA) ([]ports.CheckRun, error) {
	base, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	if err := checkSHA(sha); err != nil {
		return nil, err
	}
	token, err := a.tokens.Reader(ctx)
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("%s/commits/%s/check-runs", base, sha)
	items, err := listAllCheckRuns(ctx, a.c, token, path, url.Values{"per_page": {perPage}})
	if err != nil {
		return nil, err
	}
	out := make([]ports.CheckRun, 0, len(items))
	for _, r := range items {
		if r.Name == "heimdall/gate" {
			continue
		}
		run, err := toCheckRun(r)
		if err != nil {
			return nil, fmt.Errorf("github: check run %d: %w", r.ID, err)
		}
		out = append(out, run)
	}
	return out, nil
}

// checkRunStatus maps GitHub's status string. An unknown status is an error.
func checkRunStatus(s string) (ports.CheckStatus, error) {
	switch s {
	case "queued":
		return ports.CheckQueued, nil
	case "in_progress":
		return ports.CheckInProgress, nil
	case "completed":
		return ports.CheckCompleted, nil
	}
	return "", fmt.Errorf("unknown status %q", s)
}

// checkRunConclusion maps GitHub's conclusion for completed runs. An unknown conclusion is an error.
func checkRunConclusion(status ports.CheckStatus, c string) (change.CIConclusion, error) {
	if status != ports.CheckCompleted {
		return "", nil
	}
	switch c {
	case "success":
		return change.CISuccess, nil
	case "failure":
		return change.CIFailure, nil
	case "neutral":
		return change.CINeutral, nil
	case "cancelled":
		return change.CICancelled, nil
	case "skipped":
		return change.CISkipped, nil
	case "timed_out":
		return change.CITimedOut, nil
	case "action_required":
		return change.CIActionRequired, nil
	}
	return "", fmt.Errorf("unknown conclusion %q", c)
}

// toCheckRun maps a GitHub check run item to ports.CheckRun.
func toCheckRun(r checkRunItemJSON) (ports.CheckRun, error) {
	runSHA, err := id.ParseSHA(r.HeadSHA)
	if err != nil {
		return ports.CheckRun{}, err
	}
	status, err := checkRunStatus(r.Status)
	if err != nil {
		return ports.CheckRun{}, err
	}
	conclusion, err := checkRunConclusion(status, r.Conclusion)
	if err != nil {
		return ports.CheckRun{}, err
	}
	run := ports.CheckRun{
		ID:         strconv.FormatInt(r.ID, 10),
		Name:       r.Name,
		Status:     status,
		Conclusion: conclusion,
		SHA:        runSHA,
	}
	if err := run.Validate(); err != nil {
		return ports.CheckRun{}, err
	}
	return run, nil
}
