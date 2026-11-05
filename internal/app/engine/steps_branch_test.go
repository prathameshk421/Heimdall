package engine_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/engine"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// branchStepFrom builds the branch step from a with object and fails the test when it is refused.
func branchStepFrom(t *testing.T, with string) engine.EnsureBranch {
	t.Helper()
	s, err := engine.NewEnsureBranch(json.RawMessage(with))
	require.NoError(t, err)
	return s
}

func TestEnsureBranchTypeDoneAndEffect(t *testing.T) {
	s := branchStepFrom(t, `{"name":"{ticket}-{slug}"}`)
	require.Equal(t, "github.ensure_branch", s.Type())

	trig := roomTriggered(t)
	rm := roomOpenedEvt(t)
	require.False(t, s.Done(roomView(trig, rm)))

	res, err := s.OnEvent(context.Background(), roomView(trig, rm), rm)
	require.NoError(t, err)
	require.Empty(t, res.Events)
	require.Len(t, res.Effects, 1)

	fx := res.Effects[0]
	require.Equal(t, change.EffectGitHubBranch, fx.Kind)
	require.Equal(t, string(roomTicketKey), fx.Target)
	wantKey, err := change.IdempotencyKey(roomTicketKey, rm.Version, change.EffectGitHubBranch, "branch")
	require.NoError(t, err)
	require.Equal(t, wantKey, fx.Key)
	require.NotEqual(t, change.EffectRoomOpen, fx.Kind)

	p, err := change.DecodeBranchPayload(fx.Payload)
	require.NoError(t, err)
	require.Equal(t, change.BranchPayload{Repo: roomRepoName, Ticket: roomTicketKey, Template: "{ticket}-{slug}"}, p)

	require.True(t, s.Done(roomView(trig, rm, roomBranchEnsuredEvt(t))))
}

func TestEnsureBranchDefaultTemplate(t *testing.T) {
	s := branchStepFrom(t, `{}`)
	trig := roomTriggered(t)
	rm := roomOpenedEvt(t)
	res, err := s.OnEvent(context.Background(), roomView(trig, rm), rm)
	require.NoError(t, err)
	require.Len(t, res.Effects, 1)
	p, err := change.DecodeBranchPayload(res.Effects[0].Payload)
	require.NoError(t, err)
	require.Equal(t, change.DefaultBranchTemplate, p.Template)
	require.Equal(t, "{ticket}-{slug}", p.Template)
}

func TestEnsureBranchStartAndTimeoutReturnNothing(t *testing.T) {
	s := branchStepFrom(t, `{}`)
	res, err := s.Start(context.Background(), roomView(roomTriggered(t)), json.RawMessage(`{}`))
	require.NoError(t, err)
	require.Equal(t, engine.Result{}, res)
	res, err = s.OnTimeout(context.Background(), roomView(roomTriggered(t)))
	require.NoError(t, err)
	require.Equal(t, engine.Result{}, res)
}

func TestEnsureBranchIgnoresOtherEvents(t *testing.T) {
	s := branchStepFrom(t, `{}`)
	trig := roomTriggered(t)
	res, err := s.OnEvent(context.Background(), roomView(trig), trig)
	require.NoError(t, err)
	require.Empty(t, res.Effects, "change.triggered is not the event this step waits for")
}

func TestEnsureBranchNeedsTheTriggerInTheView(t *testing.T) {
	s := branchStepFrom(t, `{}`)
	rm := roomOpenedEvt(t)
	_, err := s.OnEvent(context.Background(), roomView(rm), rm)
	require.Error(t, err, "the repository comes from change.triggered, so a view without it is refused")
}

func TestEnsureBranchRefusesTemplatesThatBreakTheNameRule(t *testing.T) {
	for name, with := range map[string]string{
		"no ticket prefix":    `{"name":"feature-{slug}"}`,
		"ticket without dash": `{"name":"{ticket}{slug}"}`,
		"unknown placeholder": `{"name":"{ticket}-{owner}"}`,
		"unsafe character":    `{"name":"{ticket}-a b"}`,
		"double dot":          `{"name":"{ticket}-a..b"}`,
		"unknown key":         `{"name":"{ticket}-{slug}","base":"main"}`,
		"name not a string":   `{"name":3}`,
		"empty name":          `{"name":""}`,
		"empty with":          ``,
		"too long template":   `{"name":"{ticket}-` + repeatA(200) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := engine.NewEnsureBranch(json.RawMessage(with))
			require.Error(t, err)
		})
	}
}

func repeatA(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}
