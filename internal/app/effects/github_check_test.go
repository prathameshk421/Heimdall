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
	t.Parallel()
	p := validCheck()
	fx := checkEffect(t, p, string(p.Repo))
	checks := &fakeChecks{}
	g := newGitHubCheck(t, checks)

	require.NoError(t, g.Execute(context.Background(), fx))
	require.Equal(t, 1, checks.calls)
	require.Equal(t, checkRepo, checks.repo)
	require.Equal(t, checkSHA, checks.sha)
	require.Equal(t, "heimdall/gate", checks.check.Name)
	require.Equal(t, ports.CheckCompleted, checks.check.Status)
	require.Equal(t, change.CISuccess, checks.check.Conclusion)
	require.Equal(t, "the merge rule passed", checks.check.Summary)
	require.Equal(t, "github_check:PAY-1:3:acme/pay:abcdefabcdefabcdefabcdefabcdefabcdefabcd:1", checks.check.ExternalID)
}

// Every refusal names what is wrong and writes nothing.
func TestGitHubCheckRefusesBadEffects(t *testing.T) {
	t.Parallel()
	valid := `{"repo":"acme/pay","sha":"` + string(checkSHA) +
		`","name":"heimdall/gate","status":"completed","conclusion":"success","summary":"ok"}`
	overLong := validCheck()
	overLong.Summary = strings.Repeat("x", effects.MaxCheckSummary+1)
	cases := map[string]struct {
		fx   change.Effect
		want string
	}{
		"another kind": {func() change.Effect {
			fx := checkEffect(t, validCheck(), string(checkRepo))
			fx.Kind = change.EffectGitHubBranch
			return fx
		}(), "github_check"},
		"another target": {checkEffect(t, validCheck(), "other/repo"), "target"},
		"unknown field": {change.Effect{Key: "github_check:PAY-1:3:acme/pay:abc:1", Kind: change.EffectGitHubCheck, Target: string(checkRepo),
			Payload: json.RawMessage(valid[:len(valid)-1] + `,"extra":true}`)}, "payload"},
		"trailing data": {change.Effect{Key: "github_check:PAY-1:3:acme/pay:abc:1", Kind: change.EffectGitHubCheck, Target: string(checkRepo),
			Payload: json.RawMessage(valid + `{}`)}, "payload"},
		"duplicate key": {change.Effect{Key: "github_check:PAY-1:3:acme/pay:abc:1", Kind: change.EffectGitHubCheck, Target: string(checkRepo),
			Payload: json.RawMessage(`{"repo":"acme/pay","repo":"acme/pay","sha":"` + string(checkSHA) +
				`","name":"heimdall/gate","status":"completed","conclusion":"success","summary":"ok"}`)}, "payload"},
		"not an object": {change.Effect{Key: "github_check:PAY-1:3:acme/pay:abc:1", Kind: change.EffectGitHubCheck, Target: string(checkRepo),
			Payload: json.RawMessage(`[]`)}, "payload"},
		"summary over the bound": {checkEffect(t, overLong, string(checkRepo)), "payload"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			checks := &fakeChecks{}
			g := newGitHubCheck(t, checks)
			err := g.Execute(context.Background(), c.fx)
			require.Error(t, err)
			require.Contains(t, err.Error(), c.want)
			require.Zero(t, checks.calls)
		})
	}
}

func TestGitHubCheckPayloadValidatesStatusAndConclusion(t *testing.T) {
	t.Parallel()
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

func TestGitHubCheckKeepsTheCauseOutOfTheErrorText(t *testing.T) {
	t.Parallel()
	checks := &fakeChecks{err: errors.New(canaryTxt)}
	g := newGitHubCheck(t, checks)
	fx := checkEffect(t, validCheck(), string(checkRepo))

	err := g.Execute(context.Background(), fx)
	require.Error(t, err)
	require.ErrorIs(t, err, checks.err)
	require.NotContains(t, err.Error(), canaryTxt)
	require.Contains(t, err.Error(), "github_check")
}
