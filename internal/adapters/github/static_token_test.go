package github

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

const (
	staticTok    = "ghs_static_token_value"
	actionAuthor = "github-actions[bot]"
)

// actionEnv is an ActionAPI that talks to the fake with a static token. No token mint route exists, so a call that
// tries to mint one fails the test as an unscripted request.
type actionEnv struct {
	f *fakeGitHub
	x *ActionAPI
}

func newActionEnv(t *testing.T) *actionEnv {
	t.Helper()
	f := newFake(t)
	clock := &fakeClock{now: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)}
	w := &fakeWait{clock: clock}
	tok, err := NewStaticToken(staticTok)
	if err != nil {
		t.Fatalf("NewStaticToken: %v", err)
	}
	x, err := NewAction(ActionConfig{BaseURL: f.srv.URL, Tokens: tok, HTTPClient: f.srv.Client(), Clock: clock, Wait: w.wait, Author: actionAuthor})
	if err != nil {
		t.Fatalf("NewAction: %v", err)
	}
	return &actionEnv{f: f, x: x}
}

// wantAuth fails the test unless every recorded request carried the static token.
func (e *actionEnv) wantAuth(t *testing.T) {
	t.Helper()
	reqs := e.f.requests()
	if len(reqs) == 0 {
		t.Fatal("no request was sent")
	}
	for _, r := range reqs {
		if r.Auth != "Bearer "+staticTok {
			t.Errorf("%s %s carried auth %q", r.Method, r.Path, r.Auth)
		}
	}
}

func TestStaticTokenRefusesABadToken(t *testing.T) {
	for _, bad := range []string{"", "a b", "tok\n", "tok\x00", strings.Repeat("x", maxStaticTokenLen+1)} {
		_, err := NewStaticToken(bad)
		if err == nil {
			t.Errorf("token %q was accepted", bad)
			continue
		}
		if bad != "" && strings.Contains(err.Error(), bad) {
			t.Errorf("error carries the token: %v", err)
		}
	}
}

func TestStaticTokenNeverPrintsTheToken(t *testing.T) {
	tok, err := NewStaticToken(staticTok)
	if err != nil {
		t.Fatal(err)
	}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		if got := fmt.Sprintf(verb, tok); strings.Contains(got, staticTok) {
			t.Errorf("%s printed the token: %s", verb, got)
		}
	}
	if got := fmt.Sprintf("%+v", struct{ T *StaticToken }{tok}); strings.Contains(got, staticTok) {
		t.Errorf("nested print carried the token: %s", got)
	}
}

func TestStaticTokenWriterChecksOperationAndRepo(t *testing.T) {
	tok, err := NewStaticToken(staticTok)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if got, err := tok.Writer(ctx, OpCommentWrite, testRepo); err != nil || got != staticTok {
		t.Errorf("Writer = %q, %v; want the token", got, err)
	}
	if _, err := tok.Writer(ctx, Op("delete_everything"), testRepo); err == nil {
		t.Error("an unknown operation was accepted")
	}
	if _, err := tok.Writer(ctx, OpBranchWrite, change.Repo("not-a-repo")); err == nil {
		t.Error("a bad repository was accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := tok.Reader(cancelled); err == nil {
		t.Error("a cancelled context was accepted")
	}
}

func TestNewActionNeedsItsDependencies(t *testing.T) {
	if _, err := NewAction(ActionConfig{BaseURL: "http://x"}); err == nil {
		t.Error("missing dependencies were accepted")
	}
}

func TestAdapterActionUsesTheAppTokenSource(t *testing.T) {
	env := newTestEnv(t)
	env.f.route("GET", issueCommentsPath, func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, []any{}) })
	if _, err := env.a.Action(actionAuthor).IssueComments(context.Background(), testRepo, 7); err != nil {
		t.Fatalf("IssueComments: %v", err)
	}
	if *env.mints != 1 {
		t.Errorf("minted %d tokens, want 1", *env.mints)
	}
	last := env.f.requests()[len(env.f.requests())-1]
	if last.Auth != "Bearer ghs_1" {
		t.Errorf("auth = %q, want the minted installation token", last.Auth)
	}
}
