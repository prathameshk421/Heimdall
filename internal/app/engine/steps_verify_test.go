package engine_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/engine"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

const (
	testRepo    = change.Repo("acme/pay-demo")
	testTicket  = id.ChangeID("PAY-142")
	testHead    = id.SHA("0123456789abcdef0123456789abcdef01234567")
	testNewHead = id.SHA("fedcba9876543210fedcba9876543210fedcba98")
	testFnd1    = id.FindingID("fnd_01HZX3K9QW7M2P5R8T0V4YB6CD")
	testFnd2    = id.FindingID("fnd_01HZX3K9QW7M2P5R8T0V4YB6CE")
)

func sampleBaseEvents() []change.Event {
	return []change.Event{
		{
			ID:       "ev-1",
			ChangeID: testTicket,
			Version:  1,
			Type:     change.EventChangeTriggered,
			Payload:  json.RawMessage(`{"ticket":"PAY-142","repo":"` + string(testRepo) + `","assignee":"person:alice","source":"jira"}`),
		},
		{
			ID:       "ev-2",
			ChangeID: testTicket,
			Version:  2,
			Type:     change.EventPROpened,
			Payload:  json.RawMessage(`{"repo":"` + string(testRepo) + `","number":42,"head_sha":"` + string(testHead) + `","author":"ana","ticket":"PAY-142"}`),
		},
	}
}

func sampleFindingFiledEvent(fndID id.FindingID, ref string, tier finding.Tier, version int64) change.Event {
	src := base64.StdEncoding.EncodeToString([]byte("package test\nfunc TestRepro(t *testing.T){}\n"))
	payload := `{
		"finding_id":"` + string(fndID) + `",
		"ref":"` + ref + `",
		"tier":"` + string(tier) + `","severity":"high",
		"state":"open",
		"by":"reviewer-agent",
		"test_path":"tests/repro_test.go",
		"test_source":"` + src + `",
		"test_name":"TestRepro"
	}`
	return change.Event{
		ID:       change.EventID("ev-finding-" + string(fndID)),
		ChangeID: testTicket,
		Version:  version,
		Type:     change.EventFindingFiled,
		Payload:  json.RawMessage(payload),
	}
}

func TestVerifyEvidenceDoneWhenNoE1Waiting(t *testing.T) {
	step, err := engine.NewVerifyEvidence(nil)
	require.NoError(t, err)

	// 1. Empty events: done is true
	v := engine.ChangeView{ID: testTicket, Events: sampleBaseEvents()}
	require.True(t, step.Done(v))

	// 2. A finding the server cannot repeat (E4): done is still true
	v.Events = append(v.Events, sampleFindingFiledEvent(testFnd1, "F1", finding.TierE4, 3))
	require.True(t, step.Done(v))

	// 3. E1 finding filed: not done
	v.Events = append(v.Events, sampleFindingFiledEvent(testFnd2, "F2", finding.TierE1, 4))
	require.False(t, step.Done(v))

	// 4. E1 finding verified: done is true
	verEv := change.Event{
		ID:       "ev-ver-1",
		ChangeID: testTicket,
		Version:  5,
		Type:     change.EventFindingVerified,
		Payload:  json.RawMessage(`{"finding_id":"` + string(testFnd2) + `","ref":"F2","tier":"E1","severity":"high","state":"open","by":"heimdall"}`),
	}
	v.Events = append(v.Events, verEv)
	require.True(t, step.Done(v))

	// 5. PR pushed: re-opens verification, so not done
	pushEv := change.Event{
		ID:       "ev-push-1",
		ChangeID: testTicket,
		Version:  6,
		Type:     change.EventPRPushed,
		Payload:  json.RawMessage(`{"repo":"` + string(testRepo) + `","number":42,"head_sha":"` + string(testNewHead) + `"}`),
	}
	v.Events = append(v.Events, pushEv)
	require.False(t, step.Done(v))

	// 6. E1 finding verified on new head: done is true again
	verEv2 := change.Event{
		ID:       "ev-ver-2",
		ChangeID: testTicket,
		Version:  7,
		Type:     change.EventFindingVerified,
		Payload:  json.RawMessage(`{"finding_id":"` + string(testFnd2) + `","ref":"F2","tier":"E1","severity":"high","state":"open","by":"heimdall"}`),
	}
	v.Events = append(v.Events, verEv2)
	require.True(t, step.Done(v))
}

func TestVerifyEvidenceOnFindingFiledE1ReturnsEffect(t *testing.T) {
	step, err := engine.NewVerifyEvidence(nil)
	require.NoError(t, err)

	evs := sampleBaseEvents()
	filedEv := sampleFindingFiledEvent(testFnd1, "F1", finding.TierE1, 3)
	evs = append(evs, filedEv)
	view := engine.ChangeView{ID: testTicket, Events: evs}

	res, err := step.OnEvent(context.Background(), view, filedEv)
	require.NoError(t, err)
	require.Len(t, res.Effects, 1)

	fx := res.Effects[0]
	require.Equal(t, change.EffectVerifyEvidence, fx.Kind)
	require.Equal(t, string(testTicket), fx.Target)

	p, err := change.DecodeVerifyEvidencePayload(fx.Payload)
	require.NoError(t, err)
	require.Equal(t, testRepo, p.Repo)
	require.Equal(t, testTicket, p.Ticket)
	require.Equal(t, testFnd1, p.FindingID)
	require.Equal(t, id.FindingRef(1), p.FindingRef)
	require.Equal(t, testHead, p.HeadSHA)
	require.Equal(t, "tests/repro_test.go", p.TestPath)
	require.Equal(t, "TestRepro", p.TestName)
}

func TestVerifyEvidenceOnFindingFiledE4NoEffect(t *testing.T) {
	step, err := engine.NewVerifyEvidence(nil)
	require.NoError(t, err)

	for _, tier := range []finding.Tier{finding.TierE4} {
		evs := sampleBaseEvents()
		filedEv := sampleFindingFiledEvent(testFnd1, "F1", tier, 3)
		evs = append(evs, filedEv)
		view := engine.ChangeView{ID: testTicket, Events: evs}

		res, err := step.OnEvent(context.Background(), view, filedEv)
		require.NoError(t, err)
		require.Empty(t, res.Effects)
	}
}

func TestVerifyEvidenceOnFindingFiledAlreadyVerifiedNoEffect(t *testing.T) {
	step, err := engine.NewVerifyEvidence(nil)
	require.NoError(t, err)

	evs := sampleBaseEvents()
	filedEv := sampleFindingFiledEvent(testFnd1, "F1", finding.TierE1, 3)
	verEv := change.Event{
		ID:       "ev-ver-1",
		ChangeID: testTicket,
		Version:  4,
		Type:     change.EventFindingVerified,
		Payload:  json.RawMessage(`{"finding_id":"` + string(testFnd1) + `","ref":"F1","tier":"E1","severity":"high","state":"open","by":"heimdall"}`),
	}
	evs = append(evs, filedEv, verEv)
	view := engine.ChangeView{ID: testTicket, Events: evs}

	res, err := step.OnEvent(context.Background(), view, filedEv)
	require.NoError(t, err)
	require.Empty(t, res.Effects)
}

func TestVerifyEvidenceOnPRPushedReRequestsEveryOpenE1Finding(t *testing.T) {
	step, err := engine.NewVerifyEvidence(nil)
	require.NoError(t, err)

	evs := sampleBaseEvents()
	fnd1 := sampleFindingFiledEvent(testFnd1, "F1", finding.TierE1, 3)
	fnd2 := sampleFindingFiledEvent(testFnd2, "F2", finding.TierE1, 4)
	evs = append(evs, fnd1, fnd2)

	pushEv := change.Event{
		ID:       "ev-push-1",
		ChangeID: testTicket,
		Version:  5,
		Type:     change.EventPRPushed,
		Payload:  json.RawMessage(`{"repo":"` + string(testRepo) + `","number":42,"head_sha":"` + string(testNewHead) + `"}`),
	}
	evs = append(evs, pushEv)
	view := engine.ChangeView{ID: testTicket, Events: evs}

	res, err := step.OnEvent(context.Background(), view, pushEv)
	require.NoError(t, err)
	require.Len(t, res.Effects, 2)

	p1, err := change.DecodeVerifyEvidencePayload(res.Effects[0].Payload)
	require.NoError(t, err)
	require.Equal(t, testNewHead, p1.HeadSHA)
	require.Equal(t, testFnd1, p1.FindingID)

	p2, err := change.DecodeVerifyEvidencePayload(res.Effects[1].Payload)
	require.NoError(t, err)
	require.Equal(t, testNewHead, p2.HeadSHA)
	require.Equal(t, testFnd2, p2.FindingID)
}

func TestVerifyEvidenceMissingReproTestFails(t *testing.T) {
	step, err := engine.NewVerifyEvidence(nil)
	require.NoError(t, err)

	evs := sampleBaseEvents()
	// An E1 finding without test_source/repro_test
	barePayload := `{
		"finding_id":"` + string(testFnd1) + `",
		"ref":"F1",
		"tier":"E1","severity":"high",
		"state":"open",
		"by":"reviewer-agent"
	}`
	filedEv := change.Event{
		ID:       "ev-fnd-1",
		ChangeID: testTicket,
		Version:  3,
		Type:     change.EventFindingFiled,
		Payload:  json.RawMessage(barePayload),
	}
	evs = append(evs, filedEv)
	view := engine.ChangeView{ID: testTicket, Events: evs}

	_, err = step.OnEvent(context.Background(), view, filedEv)
	require.Error(t, err)
	require.Contains(t, err.Error(), "repro test source")
}

func TestVerifyEvidenceReplayDeterminism(t *testing.T) {
	step, err := engine.NewVerifyEvidence(nil)
	require.NoError(t, err)

	evs := sampleBaseEvents()
	filedEv := sampleFindingFiledEvent(testFnd1, "F1", finding.TierE1, 3)
	evs = append(evs, filedEv)
	view := engine.ChangeView{ID: testTicket, Events: evs}

	res1, err := step.OnEvent(context.Background(), view, filedEv)
	require.NoError(t, err)

	res2, err := step.OnEvent(context.Background(), view, filedEv)
	require.NoError(t, err)

	require.Equal(t, res1, res2)
}

func sampleE2FindingFiledEvent() change.Event {
	fndID, ref, version := testFnd1, "F1", int64(3)
	payload := `{
		"finding_id":"` + string(fndID) + `",
		"ref":"` + ref + `",
		"tier":"E2","severity":"high",
		"state":"open",
		"by":"reviewer-agent",
		"tool_result":{"tool":"go vet","args":[],"location":{"file":"internal/pay/refund.go","lines":[10,12]}}
	}`
	return change.Event{
		ID:       change.EventID("ev-finding-" + string(fndID)),
		ChangeID: testTicket,
		Version:  version,
		Type:     change.EventFindingFiled,
		Payload:  json.RawMessage(payload),
	}
}

// An E2 finding is verified by re-running its tool on the head: the effect carries the check and the location, and no
// repro test (A2).
func TestVerifyEvidenceOnFindingFiledE2RequestsACheck(t *testing.T) {
	step, err := engine.NewVerifyEvidence(nil)
	require.NoError(t, err)
	filedEv := sampleE2FindingFiledEvent()
	view := engine.ChangeView{ID: testTicket, Events: append(sampleBaseEvents(), filedEv)}

	res, err := step.OnEvent(context.Background(), view, filedEv)
	require.NoError(t, err)
	require.Len(t, res.Effects, 1)
	p, err := change.DecodeVerifyEvidencePayload(res.Effects[0].Payload)
	require.NoError(t, err)
	require.NotNil(t, p.Check)
	require.Equal(t, change.CheckGoVet, p.Check.Name)
	require.Equal(t, "internal/pay/refund.go", p.Check.File)
	require.Equal(t, [2]int{10, 12}, p.Check.Lines)
	require.Empty(t, p.TestPath)
	require.False(t, step.Done(view), "the E2 finding waits for its verification")
}

// An E2 finding with no tool result on its event cannot be verified, and the step says so instead of guessing.
func TestVerifyEvidenceE2WithoutAToolResultFails(t *testing.T) {
	step, err := engine.NewVerifyEvidence(nil)
	require.NoError(t, err)
	filedEv := sampleFindingFiledEvent(testFnd1, "F1", finding.TierE2, 3)
	view := engine.ChangeView{ID: testTicket, Events: append(sampleBaseEvents(), filedEv)}

	_, err = step.OnEvent(context.Background(), view, filedEv)
	require.ErrorContains(t, err, "no tool result")
}

// Every push re-runs the open E2 findings as well as the E1 ones.
func TestVerifyEvidenceOnPRPushedReRequestsOpenE2Findings(t *testing.T) {
	step, err := engine.NewVerifyEvidence(nil)
	require.NoError(t, err)
	filedEv := sampleE2FindingFiledEvent()
	verEv := change.Event{
		ID: "ev-ver-e2", ChangeID: testTicket, Version: 4, Type: change.EventFindingVerified,
		Payload: json.RawMessage(`{"finding_id":"` + string(testFnd1) + `","ref":"F1","tier":"E2","severity":"high","state":"open","by":"heimdall"}`),
	}
	pushEv := change.Event{
		ID: "ev-push-e2", ChangeID: testTicket, Version: 5, Type: change.EventPRPushed,
		Payload: json.RawMessage(`{"repo":"` + string(testRepo) + `","number":42,"head_sha":"fedcba9876543210fedcba9876543210fedcba98"}`),
	}
	view := engine.ChangeView{ID: testTicket, Events: append(sampleBaseEvents(), filedEv, verEv, pushEv)}

	res, err := step.OnEvent(context.Background(), view, pushEv)
	require.NoError(t, err)
	require.Len(t, res.Effects, 1)
	p, err := change.DecodeVerifyEvidencePayload(res.Effects[0].Payload)
	require.NoError(t, err)
	require.NotNil(t, p.Check)
	require.Equal(t, id.SHA("fedcba9876543210fedcba9876543210fedcba98"), p.HeadSHA)
}

func sampleE3FindingFiledEvent(fndID id.FindingID, ref string, version int64, memoryRef string) change.Event {
	mem := ""
	if memoryRef != "" {
		mem = `,"memory_ref":"` + memoryRef + `"`
	}
	return change.Event{
		ID: change.EventID("ev-finding-" + string(fndID)), ChangeID: testTicket, Version: version, Type: change.EventFindingFiled,
		Payload: json.RawMessage(`{"finding_id":"` + string(fndID) + `","ref":"` + ref + `","tier":"E3","severity":"high","state":"open","by":"bob-reviewer"` + mem + `}`),
	}
}

// An E3 finding is verified by looking its memory reference up again with the scopes of the reviewer that cited it.
func TestVerifyEvidenceOnFindingFiledE3RequestsAMemoryRecheck(t *testing.T) {
	step, err := engine.NewVerifyEvidence(nil)
	require.NoError(t, err)
	filedEv := sampleE3FindingFiledEvent(testFnd1, "F1", 3, "decision:refund-limit")
	view := engine.ChangeView{ID: testTicket, Events: append(sampleBaseEvents(), filedEv)}

	res, err := step.OnEvent(context.Background(), view, filedEv)
	require.NoError(t, err)
	require.Len(t, res.Effects, 1)
	p, err := change.DecodeVerifyEvidencePayload(res.Effects[0].Payload)
	require.NoError(t, err)
	require.NotNil(t, p.Memory)
	require.Equal(t, "decision:refund-limit", p.Memory.Ref)
	require.Equal(t, "bob-reviewer", p.Memory.Agent)
	require.Nil(t, p.Check)
	require.False(t, step.Done(view), "the E3 finding waits for its re-query")
}

func TestVerifyEvidenceE3WithoutAMemoryRefFails(t *testing.T) {
	step, err := engine.NewVerifyEvidence(nil)
	require.NoError(t, err)
	filedEv := sampleE3FindingFiledEvent(testFnd1, "F1", 3, "")
	view := engine.ChangeView{ID: testTicket, Events: append(sampleBaseEvents(), filedEv)}

	_, err = step.OnEvent(context.Background(), view, filedEv)
	require.ErrorContains(t, err, "no memory reference")
}

// The verify step opens only after the review step is done, so it never sees the findings as they are filed. The last
// review.submitted opens it, and it then requests verification for every finding still waiting (A2).
func TestVerifyEvidenceOnReviewSubmittedRequestsEveryWaitingFinding(t *testing.T) {
	step, err := engine.NewVerifyEvidence(nil)
	require.NoError(t, err)
	e2 := sampleE2FindingFiledEvent()
	e1 := sampleFindingFiledEvent(testFnd2, "F2", finding.TierE1, 4)
	submitted := change.Event{
		ID: "ev-review-done", ChangeID: testTicket, Version: 5, Type: change.EventReviewSubmitted,
		Payload: json.RawMessage(`{"reviewer":"ana-reviewer","finding_refs":["F1","F2"],"pack_date":"2026-11-06"}`),
	}
	view := engine.ChangeView{ID: testTicket, Events: append(sampleBaseEvents(), e2, e1, submitted)}

	res, err := step.OnEvent(context.Background(), view, submitted)
	require.NoError(t, err)
	require.Len(t, res.Effects, 2, "one verification for each waiting finding")
	kinds := map[id.FindingID]bool{}
	for _, fx := range res.Effects {
		p, err := change.DecodeVerifyEvidencePayload(fx.Payload)
		require.NoError(t, err)
		kinds[p.FindingID] = p.Check != nil
	}
	require.True(t, kinds[testFnd1], "the E2 finding asks for a check")
	require.False(t, kinds[testFnd2], "the E1 finding asks for its repro test")
}

// A finding that already has a verification since the last push is not requested again.
func TestVerifyEvidenceOnReviewSubmittedSkipsVerifiedFindings(t *testing.T) {
	step, err := engine.NewVerifyEvidence(nil)
	require.NoError(t, err)
	filed := sampleE2FindingFiledEvent()
	verified := change.Event{
		ID: "ev-ver", ChangeID: testTicket, Version: 4, Type: change.EventFindingVerified,
		Payload: json.RawMessage(`{"finding_id":"` + string(testFnd1) + `","ref":"F1","tier":"E2","severity":"high","state":"open","by":"heimdall"}`),
	}
	submitted := change.Event{
		ID: "ev-review-done", ChangeID: testTicket, Version: 5, Type: change.EventReviewSubmitted,
		Payload: json.RawMessage(`{"reviewer":"ana-reviewer","finding_refs":["F1"],"pack_date":"2026-11-06"}`),
	}
	view := engine.ChangeView{ID: testTicket, Events: append(sampleBaseEvents(), filed, verified, submitted)}

	res, err := step.OnEvent(context.Background(), view, submitted)
	require.NoError(t, err)
	require.Empty(t, res.Effects)
}

func recheckEvent(fndID id.FindingID, ref string, version int64) change.Event {
	return change.Event{
		ID:       change.EventID("ev-recheck-" + string(fndID)),
		ChangeID: testTicket,
		Version:  version,
		Type:     change.EventFindingRecheckRequested,
		Payload:  json.RawMessage(`{"finding_id":"` + string(fndID) + `","ref":"` + ref + `","reason":"the runner was busy","by":"operator"}`),
	}
}

func unverifiedEvent(fndID id.FindingID, ref string, version int64) change.Event {
	return change.Event{
		ID:       change.EventID("ev-unverified-" + string(fndID)),
		ChangeID: testTicket,
		Version:  version,
		Type:     change.EventFindingStateChanged,
		Payload:  json.RawMessage(`{"finding_id":"` + string(fndID) + `","ref":"` + ref + `","tier":"E4","severity":"high","state":"unverified","by":"heimdall"}`),
	}
}

func TestVerifyEvidenceRecheckRepeatsTheEvidenceOfAFindingDroppedToE4(t *testing.T) {
	step, err := engine.NewVerifyEvidence(nil)
	require.NoError(t, err)

	filed := sampleFindingFiledEvent(testFnd1, "F4", finding.TierE1, 3)
	evs := append(sampleBaseEvents(), filed, unverifiedEvent(testFnd1, "F4", 4))
	view := engine.ChangeView{ID: testTicket, Events: evs}
	require.True(t, step.Done(view), "an unverified finding waits for nothing")

	recheck := recheckEvent(testFnd1, "F4", 5)
	view.Events = append(view.Events, recheck)
	require.False(t, step.Done(view), "a recheck with no result yet keeps the step open")

	res, err := step.OnEvent(context.Background(), view, recheck)
	require.NoError(t, err)
	require.Len(t, res.Effects, 1)
	p, err := change.DecodeVerifyEvidencePayload(res.Effects[0].Payload)
	require.NoError(t, err)
	require.Equal(t, testFnd1, p.FindingID)
	require.Equal(t, testHead, p.HeadSHA, "the evidence is repeated on the current head")
	require.Equal(t, "TestRepro", p.TestName, "the repro test comes from the finding as filed")

	first, err := step.OnEvent(context.Background(), engine.ChangeView{ID: testTicket, Events: append(sampleBaseEvents(), filed)}, filed)
	require.NoError(t, err)
	require.NotEqual(t, first.Effects[0].Key, res.Effects[0].Key, "a recheck is a new effect, not the first one again")

	view.Events = append(view.Events, change.Event{
		ID: "ev-ver-recheck", ChangeID: testTicket, Version: 6, Type: change.EventFindingVerified,
		Payload: json.RawMessage(`{"finding_id":"` + string(testFnd1) + `","ref":"F4","tier":"E1","severity":"high","state":"open","by":"heimdall"}`),
	})
	require.True(t, step.Done(view), "the recheck's result ends the wait")
}

func TestVerifyEvidenceRecheckRefusesAFindingTheServerCannotRepeat(t *testing.T) {
	step, err := engine.NewVerifyEvidence(nil)
	require.NoError(t, err)

	recheck := recheckEvent(testFnd2, "F2", 4)
	evs := append(sampleBaseEvents(), sampleFindingFiledEvent(testFnd2, "F2", finding.TierE4, 3), recheck)
	_, err = step.OnEvent(context.Background(), engine.ChangeView{ID: testTicket, Events: evs}, recheck)
	require.ErrorContains(t, err, "F2")

	unknown := recheckEvent(testFnd1, "F9", 4)
	evs = append(sampleBaseEvents(), unknown)
	_, err = step.OnEvent(context.Background(), engine.ChangeView{ID: testTicket, Events: evs}, unknown)
	require.ErrorContains(t, err, "F9")
}
