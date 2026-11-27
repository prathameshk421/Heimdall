package github

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const (
	issueCommentsPath  = repoRoot + "/issues/7/comments"
	reviewCommentsPath = repoRoot + "/pulls/7/comments"
	summaryKey         = change.EffectKey("action-summary:7")
)

func marked(body string) string {
	return body + "\n\n<!-- heimdall:effect=" + string(summaryKey) + " -->"
}

func ghComment(id int, login, typ, body string) map[string]any {
	return map[string]any{
		"id": id, "body": body, "created_at": "2026-11-04T12:00:00Z", "updated_at": "2026-11-04T12:01:00Z",
		"user": map[string]any{"login": login, "type": typ},
	}
}

func TestIssueCommentsPaginateAndUseTheStaticToken(t *testing.T) {
	env := newActionEnv(t)
	next := env.f.srv.URL + issueCommentsPath + "?per_page=100&page=2"
	env.f.route("GET", issueCommentsPath, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			writeJSON(w, http.StatusOK, []any{ghComment(2, "bob", "User", "second")})
			return
		}
		w.Header().Set("Link", `<`+next+`>; rel="next"`)
		writeJSON(w, http.StatusOK, []any{ghComment(1, "ana", "User", "first")})
	})
	got, err := env.x.IssueComments(context.Background(), testRepo, 7)
	if err != nil {
		t.Fatalf("IssueComments: %v", err)
	}
	if len(got) != 2 || got[0].ID != 1 || got[1].ID != 2 || got[1].Login != "bob" {
		t.Errorf("comments = %+v, want ids 1 and 2", got)
	}
	env.wantAuth(t)
}

func TestIssueCommentsSinceSendsTheCursor(t *testing.T) {
	env := newActionEnv(t)
	env.f.route("GET", issueCommentsPath, func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, []any{}) })
	since := env.x.c.clock.Now()
	if _, err := env.x.IssueCommentsSince(context.Background(), testRepo, 7, since); err != nil {
		t.Fatalf("IssueCommentsSince: %v", err)
	}
	if q := env.f.requests()[0].Query; !strings.Contains(q, "since=2026-11-04T12%3A00%3A00Z") {
		t.Errorf("query = %q, want the since cursor", q)
	}
}

func TestPRReviewCommentsPaginateAndMapNullLines(t *testing.T) {
	env := newActionEnv(t)
	next := env.f.srv.URL + reviewCommentsPath + "?per_page=100&page=2"
	env.f.route("GET", reviewCommentsPath, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			writeJSON(w, http.StatusOK, []any{map[string]any{
				"id": 12, "body": "outdated", "path": "a.go", "line": nil, "original_line": 9, "side": "RIGHT",
				"commit_id": shaHead, "in_reply_to_id": 11, "created_at": "2026-11-04T12:00:00Z", "updated_at": "2026-11-04T12:00:00Z",
				"user": map[string]any{"login": "reviewer", "type": "Bot"},
			}})
			return
		}
		w.Header().Set("Link", `<`+next+`>; rel="next"`)
		writeJSON(w, http.StatusOK, []any{map[string]any{
			"id": 11, "body": "nit", "path": "a.go", "line": 4, "start_line": 3, "original_line": 4, "side": "RIGHT",
			"commit_id": shaHead, "created_at": "2026-11-04T12:00:00Z", "updated_at": "2026-11-04T12:00:00Z",
			"user": map[string]any{"login": "coderabbitai[bot]", "type": "Bot"},
		}})
	})
	got, err := env.x.PRReviewComments(context.Background(), testRepo, 7)
	if err != nil {
		t.Fatalf("PRReviewComments: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d comments, want 2", len(got))
	}
	if first := got[0]; first.Line != 4 || first.StartLine != 3 || first.InReplyTo != 0 || first.UserType != "Bot" {
		t.Errorf("first = %+v", first)
	}
	if second := got[1]; second.Line != 0 || second.OriginalLine != 9 || second.InReplyTo != 11 {
		t.Errorf("second = %+v, want line 0 (null), original 9, reply to 11", second)
	}
	env.wantAuth(t)
}

func TestReviewCommentsMapErrorsPerDocGo(t *testing.T) {
	cases := []struct {
		status int
		want   error
	}{
		{http.StatusUnauthorized, ports.ErrUnauthorized},
		{http.StatusForbidden, ports.ErrUnauthorized},
		{http.StatusNotFound, ports.ErrNotFound},
		{http.StatusBadGateway, ports.ErrUnavailable},
	}
	for _, tc := range cases {
		env := newActionEnv(t)
		env.f.route("GET", reviewCommentsPath, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, tc.status, map[string]string{"message": "ghs_static_token_value echoed"})
		})
		_, err := env.x.PRReviewComments(context.Background(), testRepo, 7)
		if !errors.Is(err, tc.want) {
			t.Errorf("status %d: error = %v, want %v", tc.status, err, tc.want)
		}
		if err != nil && strings.Contains(err.Error(), "echoed") {
			t.Errorf("status %d: error carries the response body: %v", tc.status, err)
		}
	}
}

func TestCommentNeverEditsAnotherAuthorsCommentThatHoldsTheMarker(t *testing.T) {
	env := newActionEnv(t)
	env.f.route("GET", issueCommentsPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, []any{
			ghComment(1, "mallory", "User", marked("quoted by a person")),
			ghComment(2, "other-app[bot]", "Bot", marked("another bot")),
		})
	})
	env.f.route("POST", issueCommentsPath, func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusCreated, map[string]any{}) })
	if err := env.x.Comment(context.Background(), testRepo, 7, summaryKey, "summary"); err != nil {
		t.Fatalf("Comment: %v", err)
	}
	for _, r := range env.f.requests() {
		if r.Method == "PATCH" {
			t.Errorf("another author's comment was edited: %s %s", r.Method, r.Path)
		}
	}
	if env.f.count("POST", issueCommentsPath) != 1 {
		t.Error("the adapter did not add its own comment")
	}
}

func TestCommentMatchesTheAuthorWithoutCase(t *testing.T) {
	env := newActionEnv(t)
	env.f.route("GET", issueCommentsPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, []any{ghComment(9, "GitHub-Actions[bot]", "Bot", marked("old"))})
	})
	env.f.route("PATCH", repoRoot+"/issues/comments/9", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, map[string]any{}) })
	if err := env.x.Comment(context.Background(), testRepo, 7, summaryKey, "new"); err != nil {
		t.Fatalf("Comment: %v", err)
	}
}

func TestCommentBoundsTheBodyBeforeAnyRequest(t *testing.T) {
	env := newActionEnv(t)
	marker := marked("")
	fits := strings.Repeat("x", MaxSummaryBodyBytes-len(marker))
	env.f.route("GET", issueCommentsPath, func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, []any{}) })
	env.f.route("POST", issueCommentsPath, func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusCreated, map[string]any{}) })
	if err := env.x.Comment(context.Background(), testRepo, 7, summaryKey, fits); err != nil {
		t.Fatalf("a body at the limit was refused: %v", err)
	}
	before := len(env.f.requests())
	err := env.x.Comment(context.Background(), testRepo, 7, summaryKey, fits+"x")
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("error = %v, want ErrBodyTooLarge", err)
	}
	if len(env.f.requests()) != before {
		t.Error("a request was sent for an oversized body")
	}
}

func TestCommentRefusesABadKeyAndAMissingAuthor(t *testing.T) {
	env := newActionEnv(t)
	if err := env.x.Comment(context.Background(), testRepo, 7, change.EffectKey("a-->b"), "x"); err == nil {
		t.Error("a key that breaks the marker was accepted")
	}
	env.x.author = ""
	if err := env.x.Comment(context.Background(), testRepo, 7, summaryKey, "x"); err == nil {
		t.Error("a missing author login was accepted")
	}
	if n := len(env.f.requests()); n != 0 {
		t.Errorf("%d requests were sent", n)
	}
}

func TestCommentWriteRefusedIsUnauthorized(t *testing.T) {
	env := newActionEnv(t)
	env.f.route("GET", issueCommentsPath, func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, []any{}) })
	env.f.route("POST", issueCommentsPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "Resource not accessible by integration"})
	})
	err := env.x.Comment(context.Background(), testRepo, 7, summaryKey, "x")
	if !errors.Is(err, ports.ErrUnauthorized) {
		t.Errorf("error = %v, want ports.ErrUnauthorized", err)
	}
}
