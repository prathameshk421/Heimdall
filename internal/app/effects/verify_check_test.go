package effects_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/effects"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const (
	checkFile = "internal/pay/refund.go"
	veBase    = id.SHA("fedcba9876543210fedcba9876543210fedcba98")
)

// patternsProfile holds the check the findings cite. baseline says whether it compares the head with the base.
func patternsProfile(baseline bool) profileFake {
	return profileFake{checks: []ports.ProfileCheck{{ID: "patterns", Lens: "security", Baseline: baseline}}}
}

// verifyCheckEffect is the job; reverify says the finding was verified on an earlier head.
func verifyCheckEffect(t *testing.T, reverify bool) change.Effect {
	t.Helper()
	ref, _ := id.ParseFindingRef("F1")
	p := change.VerifyEvidencePayload{
		Repo: veRepo, Ticket: veTicket, FindingID: veFndID, FindingRef: ref, Severity: finding.SeverityHigh, HeadSHA: veHead, BaseSHA: veBase,
		Reverify: reverify, Check: &change.VerifyCheck{ID: "patterns", File: checkFile, Lines: [2]int{10, 12}},
	}
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	key, err := change.IdempotencyKey(veTicket, 1, change.EffectVerifyEvidence, string(veFndID)+":"+string(veHead))
	require.NoError(t, err)
	return change.Effect{Key: key, Kind: change.EffectVerifyEvidence, Target: string(veTicket), Payload: raw}
}

func runVerifyCheck(t *testing.T, sb *sequenceSandbox, profile profileFake, reverify bool) (*fakeIngest, error) {
	t.Helper()
	ingest := &fakeIngest{}
	exec, err := effects.NewVerifyEvidence(effects.VerifyEvidenceDeps{
		Source: &fakeSource{}, Sandbox: sb, Profiles: profile, Ingest: ingest, IDs: &fakeIDs{}, Clock: fixedClock{},
	})
	require.NoError(t, err)
	return ingest, exec.Execute(context.Background(), verifyCheckEffect(t, reverify))
}

// headOnly answers one head run with the SARIF doc.
func headOnly(doc []byte) *sequenceSandbox {
	return &sequenceSandbox{answers: []ports.SandboxResult{{Sarif: doc}}}
}

func decodeVerifyWritten(t *testing.T, ingest *fakeIngest) (change.EventType, change.FindingRecord) {
	t.Helper()
	require.Len(t, ingest.written, 1)
	var rec change.FindingRecord
	require.NoError(t, json.Unmarshal(ingest.written[0].Payload, &rec))
	return ingest.written[0].Type, rec
}

// The first run confirms an E2 finding when the cited profile check reports a result inside the finding's file and
// lines. It runs that check on the head through the runner.
func TestVerifyCheckConfirmsALocatedResult(t *testing.T) {
	sb := headOnly(checkSarif("printf", checkFile, 11))
	ingest, err := runVerifyCheck(t, sb, patternsProfile(false), false)
	require.NoError(t, err)

	typ, rec := decodeVerifyWritten(t, ingest)
	require.Equal(t, change.EventFindingVerified, typ)
	require.Equal(t, finding.TierE2, rec.Tier)
	require.Equal(t, finding.StateOpen, rec.State)
	require.Len(t, sb.jobs, 1)
	require.Equal(t, ports.SandboxCheck, sb.jobs[0].Kind)
	require.Equal(t, "patterns", sb.jobs[0].CheckID)
	require.Equal(t, veHead, sb.jobs[0].SHA)
	require.Equal(t, ports.PriorityGate, sb.jobs[0].Priority(), "the gate waits on a verification, so it runs in the gate lane")
}

// A result somewhere else, or none, does not confirm the claim: the finding drops to E4 and never blocks.
func TestVerifyCheckWithoutALocatedResultStaysE4(t *testing.T) {
	for name, doc := range map[string][]byte{
		"a result elsewhere": checkSarif("printf", "other.go", 3),
		"a result outside":   checkSarif("printf", checkFile, 40),
		"no result":          []byte(`{"runs":[{"results":[]}]}`),
	} {
		t.Run(name, func(t *testing.T) {
			ingest, err := runVerifyCheck(t, headOnly(doc), patternsProfile(false), false)
			require.NoError(t, err)
			typ, rec := decodeVerifyWritten(t, ingest)
			require.Equal(t, change.EventFindingStateChanged, typ)
			require.Equal(t, finding.TierE4, rec.Tier)
			require.Equal(t, finding.StateUnverified, rec.State)
		})
	}
}

// With a baseline, a result the base already had is not new, so it does not confirm the finding.
func TestVerifyCheckWithABaselineCountsOnlyWhatTheHeadAdded(t *testing.T) {
	same := checkSarif("printf", checkFile, 11)
	sb := &sequenceSandbox{answers: []ports.SandboxResult{{Sarif: same}, {Sarif: same}}}
	ingest, err := runVerifyCheck(t, sb, patternsProfile(true), false)
	require.NoError(t, err)

	_, rec := decodeVerifyWritten(t, ingest)
	require.Equal(t, finding.TierE4, rec.Tier, "the base had the result, so the change did not add it")
	require.Len(t, sb.jobs, 2)
	require.Equal(t, veBase, sb.jobs[1].SHA, "the baseline run is at the base")
	require.Equal(t, veHead, sb.jobs[1].Head, "and serves the head it is compared with")

	sb = &sequenceSandbox{answers: []ports.SandboxResult{{Sarif: same}, {Sarif: []byte(`{"runs":[{"results":[]}]}`)}}}
	ingest, err = runVerifyCheck(t, sb, patternsProfile(true), false)
	require.NoError(t, err)
	_, rec = decodeVerifyWritten(t, ingest)
	require.Equal(t, finding.TierE2, rec.Tier, "the head added the result")
}

// After a push, no located result resolves the finding with no claim from the author, and a result still there keeps it
// verified so the step does not wait.
func TestVerifyCheckAfterAPushKeepsOrResolvesByLocation(t *testing.T) {
	ingest, err := runVerifyCheck(t, headOnly(checkSarif("printf", "other.go", 3)), patternsProfile(false), true)
	require.NoError(t, err)
	typ, rec := decodeVerifyWritten(t, ingest)
	require.Equal(t, change.EventFindingStateChanged, typ)
	require.Equal(t, finding.StateResolved, rec.State, "the located problem is fixed, another one is not this finding")
	require.Equal(t, finding.TierE2, rec.Tier)

	ingest, err = runVerifyCheck(t, headOnly(checkSarif("printf", checkFile, 12)), patternsProfile(false), true)
	require.NoError(t, err)
	typ, rec = decodeVerifyWritten(t, ingest)
	require.Equal(t, change.EventFindingVerified, typ)
	require.Equal(t, finding.StateOpen, rec.State)
}

// A check that could not run proves nothing either way: the effect fails so the outbox retries, and nothing is written.
func TestVerifyCheckThatCouldNotRunWritesNothing(t *testing.T) {
	ingest, err := runVerifyCheck(t, &sequenceSandbox{errs: []error{context.DeadlineExceeded}}, patternsProfile(false), false)
	require.Error(t, err)
	require.Empty(t, ingest.written)

	ingest, err = runVerifyCheck(t, headOnly([]byte("not sarif")), patternsProfile(false), false)
	require.Error(t, err, "a check with no valid SARIF gave no result")
	require.Empty(t, ingest.written)

	sb := &sequenceSandbox{errs: []error{nil, errors.New("runner down")}, answers: []ports.SandboxResult{{Sarif: checkSarif("printf", checkFile, 11)}, {}}}
	ingest, err = runVerifyCheck(t, sb, patternsProfile(true), false)
	require.Error(t, err, "a baseline run that fails is no result either")
	require.Empty(t, ingest.written)

	ingest, err = runVerifyCheck(t, headOnly(nil), profileFake{none: true}, false)
	require.Error(t, err, "without a promoted profile the check cannot run")
	require.Empty(t, ingest.written)
}

// A profile promoted after the finding may have dropped the cited check. The claim can no longer be repeated, so it is
// not evidence: the finding drops to E4 and nothing runs.
func TestVerifyCheckWhoseCheckLeftTheProfileStaysE4(t *testing.T) {
	sb := &sequenceSandbox{}
	ingest, err := runVerifyCheck(t, sb, profileFake{checks: []ports.ProfileCheck{{ID: "lint"}}}, false)
	require.NoError(t, err)
	_, rec := decodeVerifyWritten(t, ingest)
	require.Equal(t, finding.TierE4, rec.Tier)
	require.Empty(t, sb.jobs)
}

// A finding filed before profile checks names a tool (go vet) and no check id. No check repeats it, so it drops to E4
// without a run, even where the repository has no promoted profile.
func TestVerifyCheckOfAnOldToolFindingStaysE4(t *testing.T) {
	fx := verifyCheckEffect(t, false)
	var p change.VerifyEvidencePayload
	require.NoError(t, json.Unmarshal(fx.Payload, &p))
	p.Check.ID = ""
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	fx.Payload = raw

	sb := &sequenceSandbox{}
	ingest := &fakeIngest{}
	exec, err := effects.NewVerifyEvidence(effects.VerifyEvidenceDeps{
		Source: &fakeSource{}, Sandbox: sb, Profiles: profileFake{none: true}, Ingest: ingest, IDs: &fakeIDs{}, Clock: fixedClock{},
	})
	require.NoError(t, err)
	require.NoError(t, exec.Execute(context.Background(), fx))
	_, rec := decodeVerifyWritten(t, ingest)
	require.Equal(t, finding.TierE4, rec.Tier)
	require.Empty(t, sb.jobs)
}
