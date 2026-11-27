package main

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/action"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/fastcheck"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// alertReader is a ports.CodeScanReader over a fixed list, or a fixed error.
type alertReader struct {
	alerts []ports.CodeScanAlert
	err    error
	// asked records the ref and the state of every call.
	asked []string
}

func (a *alertReader) CodeScanAlerts(_ context.Context, _ change.Repo, ref, state string) ([]ports.CodeScanAlert, error) {
	a.asked = append(a.asked, ref+" "+state)
	return a.alerts, a.err
}

// analyzerRequest is a fact request with the policy's analyzer source at its default (code scanning).
func analyzerRequest() action.FactRequest {
	act := policy.Action{}.Effective()
	return action.FactRequest{Repo: change.Repo(fakeRepo), Number: 7, Head: testHead, Policy: act}
}

// TestCodeScanningLowAlertDoesNotBlock: a note is below the default medium threshold.
func TestCodeScanningLowAlertDoesNotBlock(t *testing.T) {
	r := &alertReader{alerts: []ports.CodeScanAlert{{
		Number: 42, State: "open", RuleID: "style/x", Severity: "note", SecuritySeverity: "low",
		Tool: "CodeQL", Path: "a.go", StartLine: 1, EndLine: 1}}}
	d, err := newDiffFacts(r)
	require.NoError(t, err)
	got, err := d.FastFacts(context.Background(), analyzerRequest())
	require.NoError(t, err)
	res := fastcheck.Evaluate(fastcheck.Facts{Action: analyzerRequest().Policy, Analyzers: got.Analyzers})
	require.Equal(t, fastcheck.StatusMet, res.Clauses[1].Status, res.Clauses[1].Reason)
}

// TestCodeScanningUnknownOnAForbiddenOrMissingRead: 403 and 404 cannot be told apart, so both leave the clause unknown,
// which blocks in enforce mode and never reads as a pass (fail closed).
func TestCodeScanningUnknownOnAForbiddenOrMissingRead(t *testing.T) {
	for _, want := range []error{ports.ErrUnauthorized, ports.ErrNotFound, ports.ErrUnavailable, errors.New("boom")} {
		d, err := newDiffFacts(&alertReader{err: want})
		require.NoError(t, err)
		got, err := d.FastFacts(context.Background(), analyzerRequest())
		require.NoError(t, err, "a broken analyzer feed must not fail the secret scan")
		require.True(t, got.Secrets.Known)
		require.False(t, got.Analyzers.Known)

		act := policy.Action{Mode: policy.ActionModeEnforce}
		res := fastcheck.Evaluate(fastcheck.Facts{Action: act.Effective(), Analyzers: got.Analyzers})
		require.Equal(t, fastcheck.StatusUnknown, res.Clauses[1].Status)
		require.Equal(t, "unknown", string(res.Verdict))
	}
}

// TestCodeScanningRefusesABadPullRequestNumber keeps the ref well formed: a number that is not a real pull request
// gives no ref, so no URL is built from it.
func TestCodeScanningRefusesABadPullRequestNumber(t *testing.T) {
	for _, n := range []int{0, -1, maxPRNumber + 1} {
		_, err := codeScanningRef(n)
		require.Error(t, err, n)
	}
	ref, err := codeScanningRef(7)
	require.NoError(t, err)
	require.Equal(t, "refs/pull/7/merge", ref)
}

// TestCodeScanLevelMapsBothSeverities: the policy threshold is a severity, and GitHub reports two of them.
func TestCodeScanLevelMapsBothSeverities(t *testing.T) {
	cases := map[ports.CodeScanAlert]string{
		{SecuritySeverity: "critical"}: "error",
		{SecuritySeverity: "high"}:     "error",
		{SecuritySeverity: "medium"}:   "warning",
		{SecuritySeverity: "low"}:      "note",
		{Severity: "error"}:            "error",
		{Severity: "warning"}:          "warning",
		{Severity: "note"}:             "note",
		{Severity: "none"}:             "note",
		{}:                             "warning", // neither severity: counted at the default, never ignored
	}
	for a, want := range cases {
		require.Equal(t, want, codeScanLevel(a), "%+v", a)
	}
}

// TestCodeScanResultKeepsTheAlertIdentity: the fingerprint is the alert number, so a moved alert is the same result and
// the same alert at base (if it were read) would not count as new.
func TestCodeScanResultKeepsTheAlertIdentity(t *testing.T) {
	a := ports.CodeScanAlert{Number: 9, RuleID: "go/x", Severity: "warning", Tool: "CodeQL", Path: "a.go"}
	r, ok := codeScanResult(a)
	require.True(t, ok)
	require.Equal(t, "code-scanning:CodeQL", r.Check)
	require.Equal(t, "alert:9:go/x:a.go", r.Finger)
	moved := a
	moved.StartLine = 99
	r2, _ := codeScanResult(moved)
	require.Equal(t, r.Finger, r2.Finger, "line numbers are not part of the identity")

	_, ok = codeScanResult(ports.CodeScanAlert{})
	require.False(t, ok, "an alert with no number has no identity")
	require.Equal(t, "warning", r.Level)
}
