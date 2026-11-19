package effects_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/effects"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// checkSarif is a SARIF document with one result at file:line.
func checkSarif(rule, file string, line int) []byte {
	doc := map[string]any{"runs": []any{map[string]any{"results": []any{map[string]any{
		"ruleId": rule, "level": "error", "message": map[string]any{"text": "bad " + rule},
		"locations": []any{map[string]any{"physicalLocation": map[string]any{
			"artifactLocation": map[string]any{"uri": file}, "region": map[string]any{"startLine": line, "endLine": line, "snippet": map[string]any{"text": "code " + rule}}}}},
	}}}}}
	b, _ := json.Marshal(doc)
	return b
}

// sequenceSandbox answers each job from the queue and records the jobs.
type sequenceSandbox struct {
	answers []ports.SandboxResult
	errs    []error
	jobs    []ports.SandboxJob
}

func (s *sequenceSandbox) Run(_ context.Context, j ports.SandboxJob) (ports.SandboxResult, error) {
	i := len(s.jobs)
	s.jobs = append(s.jobs, j)
	if i < len(s.errs) && s.errs[i] != nil {
		return ports.SandboxResult{}, s.errs[i]
	}
	return s.answers[i], nil
}

type profileFake struct {
	ports.ProfileStore
	checks []ports.ProfileCheck
	none   bool
}

func (p profileFake) ActiveProfile(context.Context, string) (ports.ProfileProposal, bool, error) {
	return ports.ProfileProposal{Spec: ports.ProfileSpec{Checks: p.checks}}, !p.none, nil
}

func runChecksFx(t *testing.T, ids ...string) change.Effect {
	t.Helper()
	raw, err := json.Marshal(change.RunChecksPayload{Repo: "acme/pay", Change: "PAY-1", Head: "1111111", Base: "2222222", CheckIDs: ids})
	require.NoError(t, err)
	return change.Effect{Key: "PAY-1/checks/1111111/r0", Kind: change.EffectRunChecks, Target: "acme/pay", Payload: raw}
}

func newChecks(t *testing.T, sb *sequenceSandbox, p profileFake) (effects.Checks, *fakeIngest) {
	t.Helper()
	ingest := &fakeIngest{}
	exec, err := effects.NewChecks(effects.ChecksDeps{Source: &fakeSource{}, Sandbox: sb, Profiles: p, Ingest: ingest, IDs: &fakeIDs{}, Clock: fixedClock{}})
	require.NoError(t, err)
	return exec, ingest
}

func reported(t *testing.T, ingest *fakeIngest) change.ChecksReported {
	t.Helper()
	require.Len(t, ingest.written, 1)
	require.Equal(t, change.EventChecksReported, ingest.written[0].Type)
	var r change.ChecksReported
	require.NoError(t, json.Unmarshal(ingest.written[0].Payload, &r))
	return r
}

// A check with a baseline reports only what the head added: the result the base already had is dropped.
func TestChecksReportOnlyWhatTheHeadAddedForABaselineCheck(t *testing.T) {
	old, added := checkSarif("old", "a.go", 3), checkSarif("new", "b.go", 9)
	both := map[string]any{"runs": []any{
		json.RawMessage(`{"results":` + resultsOf(t, old) + `}`),
		json.RawMessage(`{"results":` + resultsOf(t, added) + `}`),
	}}
	headSarif, _ := json.Marshal(both)
	sb := &sequenceSandbox{answers: []ports.SandboxResult{{Sarif: headSarif}, {Sarif: old}}}
	exec, ingest := newChecks(t, sb, profileFake{checks: []ports.ProfileCheck{{ID: "lint", Lens: "correctness", Baseline: true}}})

	require.NoError(t, exec.Execute(context.Background(), runChecksFx(t, "lint")))
	got := reported(t, ingest)
	require.Len(t, got.Results, 1)
	require.Equal(t, "new", got.Results[0].Rule)
	require.Equal(t, "lint", got.Results[0].Check)
	require.Empty(t, got.Errors)
	require.Len(t, sb.jobs, 2)
	require.Equal(t, id.SHA("1111111"), sb.jobs[0].SHA)
	require.Equal(t, id.SHA("2222222"), sb.jobs[1].SHA, "the baseline run is at the base")
	require.Equal(t, id.SHA("1111111"), sb.jobs[1].Head, "and serves the head it is compared with")
}

func resultsOf(t *testing.T, doc []byte) string {
	t.Helper()
	var d struct {
		Runs []struct {
			Results json.RawMessage `json:"results"`
		} `json:"runs"`
	}
	require.NoError(t, json.Unmarshal(doc, &d))
	return string(d.Runs[0].Results)
}

// A check that cannot run is an entry of errors and the job is done: the others still report.
func TestACheckThatCannotRunIsAnErrorEntryNotAFailedJob(t *testing.T) {
	sb := &sequenceSandbox{errs: []error{errors.New("runner down"), nil}, answers: []ports.SandboxResult{{}, {Sarif: checkSarif("r", "a.go", 1)}}}
	exec, ingest := newChecks(t, sb, profileFake{checks: []ports.ProfileCheck{{ID: "slow"}, {ID: "lint"}}})

	require.NoError(t, exec.Execute(context.Background(), runChecksFx(t, "slow", "lint", "gone")))
	got := reported(t, ingest)
	require.Len(t, got.Results, 1)
	require.Len(t, got.Errors, 2)
	require.Equal(t, "slow", got.Errors[0].Check)
	require.Equal(t, "gone", got.Errors[1].Check)
	require.Contains(t, got.Errors[1].Reason, "no such check")
}

func TestARepositoryWithNoPromotedProfileFailsTheJob(t *testing.T) {
	exec, ingest := newChecks(t, &sequenceSandbox{}, profileFake{none: true})
	require.Error(t, exec.Execute(context.Background(), runChecksFx(t, "lint")))
	require.Empty(t, ingest.written)
}
