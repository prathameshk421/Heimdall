package github

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

func TestPRMapsFieldsAndAuthor(t *testing.T) {
	env := newTestEnv(t)
	env.f.route("GET", "/repos/aarush323/heimdall-fixture-pay/pulls/9", func(w http.ResponseWriter, _ *http.Request) {
		pr := samplePR(9)
		pr["state"] = "closed"
		pr["merged"] = true
		writeJSON(w, http.StatusOK, pr)
	})
	got, err := env.a.PR(context.Background(), testRepo, 9)
	if err != nil {
		t.Fatalf("PR: %v", err)
	}
	if got.Repo != testRepo || got.Number != 9 || got.Branch != "feature" || got.Author != "ana" {
		t.Errorf("PR = %+v", got)
	}
	if string(got.HeadSHA) != "1111111111111111111111111111111111111111" || string(got.BaseSHA) != "2222222222222222222222222222222222222222" {
		t.Errorf("SHAs not mapped: head %s base %s", got.HeadSHA, got.BaseSHA)
	}
	if got.State != ports.PRStateClosed || !got.Merged {
		t.Errorf("state %s merged %v", got.State, got.Merged)
	}
}

func TestPRRejectsUntrustedBodyOverTheBound(t *testing.T) {
	env := newTestEnv(t)
	env.f.route("GET", "/repos/aarush323/heimdall-fixture-pay/pulls/9", func(w http.ResponseWriter, _ *http.Request) {
		pr := samplePR(9)
		pr["body"] = strings.Repeat("x", ports.MaxUntrustedText+1)
		writeJSON(w, http.StatusOK, pr)
	})
	if _, err := env.a.PR(context.Background(), testRepo, 9); err == nil {
		t.Fatal("a body over MaxUntrustedText was accepted")
	}
}

func TestFilesPaginatesAndMapsStatuses(t *testing.T) {
	env := newTestEnv(t)
	base := "/repos/aarush323/heimdall-fixture-pay/pulls/9/files"
	next := env.f.srv.URL + base + "?page=2"
	env.f.route("GET", base, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			writeJSON(w, http.StatusOK, []any{map[string]any{"filename": "c.go", "status": "removed", "additions": 0, "deletions": 4}})
			return
		}
		w.Header().Set("Link", `<`+next+`>; rel="next"`)
		writeJSON(w, http.StatusOK, []any{
			map[string]any{"filename": "a.go", "status": "added", "additions": 3, "deletions": 0},
			map[string]any{"filename": "new/b.go", "status": "renamed", "additions": 0, "deletions": 0, "previous_filename": "b.go"},
		})
	})
	files, err := env.a.Files(context.Background(), testRepo, 9)
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if len(files) != 3 || files[0].Status != ports.FileAdded || files[1].Status != ports.FileRenamed || files[1].Path != "new/b.go" || files[2].Status != ports.FileRemoved {
		t.Errorf("files = %+v", files)
	}
}

func TestFilesRefusesAnUnknownStatus(t *testing.T) {
	env := newTestEnv(t)
	env.f.route("GET", "/repos/aarush323/heimdall-fixture-pay/pulls/9/files", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, []any{map[string]any{"filename": "a.go", "status": "exploded"}})
	})
	if _, err := env.a.Files(context.Background(), testRepo, 9); err == nil {
		t.Fatal("unknown status was accepted")
	}
}

func TestReviewsAndIssueCommentsCarryLoginAndCommit(t *testing.T) {
	env := newTestEnv(t)
	env.f.route("GET", "/repos/aarush323/heimdall-fixture-pay/pulls/9/reviews", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, []any{map[string]any{
			"id": 31, "state": "APPROVED", "commit_id": "1111111111111111111111111111111111111111",
			"user": map[string]any{"login": "bob"},
		}})
	})
	env.f.route("GET", "/repos/aarush323/heimdall-fixture-pay/issues/9/comments", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, []any{map[string]any{
			"id": 4, "body": "hi", "user": map[string]any{"login": "bob", "type": "User"},
		}})
	})
	ctx := context.Background()
	reviews, err := env.a.Reviews(ctx, testRepo, 9)
	if err != nil || len(reviews) != 1 || reviews[0].Login != "bob" || reviews[0].State != "APPROVED" ||
		reviews[0].CommitID != "1111111111111111111111111111111111111111" {
		t.Fatalf("Reviews = %+v, %v", reviews, err)
	}
	comments, err := env.a.IssueComments(ctx, testRepo, 9)
	if err != nil || len(comments) != 1 || comments[0].UserType != "User" || comments[0].Body != "hi" {
		t.Fatalf("IssueComments = %+v, %v", comments, err)
	}
}

func TestPRNotFoundIsErrNotFound(t *testing.T) {
	env := newTestEnv(t)
	env.f.route("GET", "/repos/aarush323/heimdall-fixture-pay/pulls/404", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
	})
	_, err := env.a.PR(context.Background(), testRepo, 404)
	if !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("error = %v, want ports.ErrNotFound", err)
	}
}
