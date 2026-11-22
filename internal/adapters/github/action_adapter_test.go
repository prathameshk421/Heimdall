package github

import (
	"context"
	"net/http"
	"testing"
)

// The Action gate reads the pull request, its files and reviews, and posts check runs, with the workflow's static
// token. Adapter() gives it those ports on the ActionAPI's own client and token.
func TestActionAdapterReadsThePRWithTheStaticToken(t *testing.T) {
	env := newActionEnv(t)
	env.f.route("GET", repoRoot+"/pulls/9", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, samplePR(9))
	})
	a := env.x.Adapter()
	pr, err := a.PR(context.Background(), testRepo, 9)
	if err != nil {
		t.Fatalf("PR: %v", err)
	}
	if pr.Number != 9 {
		t.Errorf("PR = %+v", pr)
	}
	env.wantAuth(t)
	if a.Tokens() != nil {
		t.Error("an adapter on a static token has no App token source, so Tokens() must be nil")
	}
}

func TestUpsertSummaryReturnsTheIDOfTheNewComment(t *testing.T) {
	env := newActionEnv(t)
	env.f.route("GET", issueCommentsPath, func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, []any{}) })
	env.f.route("POST", issueCommentsPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{"id": 4242})
	})
	got, err := env.x.UpsertComment(context.Background(), testRepo, 7, summaryKey, "summary")
	if err != nil {
		t.Fatalf("UpsertComment: %v", err)
	}
	if got != 4242 {
		t.Errorf("id = %d, want 4242", got)
	}
}

func TestUpsertSummaryReturnsTheIDOfTheEditedComment(t *testing.T) {
	env := newActionEnv(t)
	env.f.route("GET", issueCommentsPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, []any{ghComment(55, actionAuthor, "Bot", marked("old"))})
	})
	env.f.route("PATCH", repoRoot+"/issues/comments/55", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"id": 55})
	})
	got, err := env.x.UpsertComment(context.Background(), testRepo, 7, summaryKey, "new")
	if err != nil {
		t.Fatalf("UpsertComment: %v", err)
	}
	if got != 55 || env.f.count("POST", issueCommentsPath) != 0 {
		t.Errorf("id = %d, POST count %d; want 55 and no POST", got, env.f.count("POST", issueCommentsPath))
	}
}

// A reply without an id would leave the log with a summary it cannot point at, so it is an error, not a zero id.
func TestUpsertSummaryRefusesAReplyWithoutAnID(t *testing.T) {
	env := newActionEnv(t)
	env.f.route("GET", issueCommentsPath, func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, []any{}) })
	env.f.route("POST", issueCommentsPath, func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusCreated, map[string]any{}) })
	if _, err := env.x.UpsertComment(context.Background(), testRepo, 7, summaryKey, "summary"); err == nil {
		t.Fatal("a reply with no comment id was accepted")
	}
}
