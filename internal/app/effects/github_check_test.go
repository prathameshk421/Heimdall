package effects_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/effects"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const (
	checkRepo = change.Repo("acme/pay")
	checkSHA  = id.SHA("abcdefabcdefabcdefabcdefabcdefabcdefabcd")
	checkKey  = id.ChangeID("PAY-1")
	canaryTxt = "CANARY-7f3a9c-do-not-print"
)

type fakeChecks struct {
	err   error
	calls int
	repo  change.Repo
	sha   id.SHA
	check ports.Check
}

func (f *fakeChecks) SetCheck(_ context.Context, repo change.Repo, sha id.SHA, c ports.Check) error {
	f.calls++
	f.repo, f.sha, f.check = repo, sha, c
	return f.err
}

func validCheck() effects.CheckPayload {
	return effects.CheckPayload{
		Repo:       checkRepo,
		SHA:        checkSHA,
		Name:       "heimdall/gate",
		Status:     change.CheckStatusCompleted,
		Conclusion: change.CISuccess,
		Summary:    "the merge rule passed",
	}
}

func checkEffect(t *testing.T, p effects.CheckPayload, target string) change.Effect {
	t.Helper()
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	key, err := change.IdempotencyKey(checkKey, 3, change.EffectGitHubCheck,
		string(p.Repo)+":"+string(p.SHA)+":1")
	require.NoError(t, err)
	return change.Effect{Key: key, Kind: change.EffectGitHubCheck, Target: target, Payload: raw}
}

func newGitHubCheck(t *testing.T, w ports.CheckWriter) effects.GitHubCheck {
	t.Helper()
	g, err := effects.NewGitHubCheck(effects.GitHubCheckDeps{Checks: w})
	require.NoError(t, err)
	return g
}

func TestGitHubCheckPublishesTheCheckWithTheEffectKey(t *testing.T) {
	p := validCheck()
	fx := checkEffect(t, p, string(p.Repo))
	checks := &fakeChecks{}
	g := newGitHubCheck(t, checks)

	require.NoError(t, g.Execute(context.Background(), fx))
	require.Equal(t, 1, checks.calls)
	require.Equal(t, change.Repo("acme/pay"), checks.repo)
	require.Equal(t, id.SHA("abcdefabcdefabcdefabcdefabcdefabcdefabcd"), checks.sha)
	require.Equal(t, "heimdall/gate", checks.check.Name)
	require.Equal(t, ports.CheckCompleted, checks.check.Status)
	require.Equal(t, change.CISuccess, checks.check.Conclusion)
	require.Equal(t, "the merge rule passed", checks.check.Summary)
	require.Equal(t, "github_check:PAY-1:3:acme/pay:abcdefabcdefabcdefabcdefabcdefabcdefabcd:1", checks.check.ExternalID)
}

func TestGitHubCheckRefusesAnotherKind(t *testing.T) {
	fx := checkEffect(t, validCheck(), string(checkRepo))
	fx.Kind = change.EffectGitHubBranch
	checks := &fakeChecks{}
	g := newGitHubCheck(t, checks)

	err := g.Execute(context.Background(), fx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "github_check")
	require.Zero(t, checks.calls)
}

func TestGitHubCheckRefusesAnotherTarget(t *testing.T) {
	fx := checkEffect(t, validCheck(), "other/repo")
	checks := &fakeChecks{}
	g := newGitHubCheck(t, checks)

	err := g.Execute(context.Background(), fx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "target")
	require.Zero(t, checks.calls)
}

func TestGitHubCheckRefusesAMalformedPayload(t *testing.T) {
	valid := `{"repo":"acme/pay","sha":"` + string(checkSHA) +
		`","name":"heimdall/gate","status":"completed","conclusion":"success","summary":"ok"}`
	cases := map[string]string{
		"unknown field": valid[:len(valid)-1] + `,"extra":true}`,
		"trailing data": valid + `{}`,
		"duplicate key": `{"repo":"acme/pay","repo":"acme/pay","sha":"` + string(checkSHA) +
			`","name":"heimdall/gate","status":"completed","conclusion":"success","summary":"ok"}`,
		"not an object": `[]`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			checks := &fakeChecks{}
			g := newGitHubCheck(t, checks)
			fx := change.Effect{
				Key:     "github_check:PAY-1:3:acme/pay:abc:1",
				Kind:    change.EffectGitHubCheck,
				Target:  string(checkRepo),
				Payload: json.RawMessage(raw),
			}
			err := g.Execute(context.Background(), fx)
			require.Error(t, err)
			require.Contains(t, err.Error(), "payload")
			require.Zero(t, checks.calls)
		})
	}
}

func TestGitHubCheckRefusesASummaryOverTheBound(t *testing.T) {
	p := validCheck()
	p.Summary = strings.Repeat("x", effects.MaxCheckSummary+1)
	fx := checkEffect(t, p, string(p.Repo))
	checks := &fakeChecks{}
	g := newGitHubCheck(t, checks)

	err := g.Execute(context.Background(), fx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "payload")
	require.Zero(t, checks.calls)
}

func TestGitHubCheckPayloadValidatesStatusAndConclusion(t *testing.T) {
	cases := map[string]effects.CheckPayload{
		"completed without a conclusion": {Repo: checkRepo, SHA: checkSHA, Name: "heimdall/gate", Status: change.CheckStatusCompleted, Summary: "ok"},
		"in progress with a conclusion": {
			Repo: checkRepo, SHA: checkSHA, Name: "heimdall/gate", Status: change.CheckStatusInProgress,
			Conclusion: change.CISuccess, Summary: "ok",
		},
		"bad status": {Repo: checkRepo, SHA: checkSHA, Name: "heimdall/gate", Status: "done", Summary: "ok"},
		"bad repo":   {Repo: "not-a-repo", SHA: checkSHA, Name: "heimdall/gate", Status: change.CheckStatusInProgress, Summary: "ok"},
		"bad sha":    {Repo: checkRepo, SHA: "zzz", Name: "heimdall/gate", Status: change.CheckStatusInProgress, Summary: "ok"},
		"empty name": {Repo: checkRepo, SHA: checkSHA, Name: " ", Status: change.CheckStatusInProgress, Summary: "ok"},
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			require.Error(t, p.Validate())
		})
	}
}

func TestGitHubCheckRefusesAMissingWriter(t *testing.T) {
	_, err := effects.NewGitHubCheck(effects.GitHubCheckDeps{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "checks")
}

func TestGitHubCheckKeepsTheCauseOutOfTheErrorText(t *testing.T) {
	checks := &fakeChecks{err: errors.New(canaryTxt)}
	g := newGitHubCheck(t, checks)
	fx := checkEffect(t, validCheck(), string(checkRepo))

	err := g.Execute(context.Background(), fx)
	require.Error(t, err)
	require.ErrorIs(t, err, checks.err)
	require.NotContains(t, err.Error(), canaryTxt)
	require.Contains(t, err.Error(), "github_check")
}
