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

	// 2. Non-E1 finding: done is still true
	v.Events = append(v.Events, sampleFindingFiledEvent(testFnd1, "F1", finding.TierE2, 3))
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

func TestVerifyEvidenceOnFindingFiledE2ToE4NoEffect(t *testing.T) {
	step, err := engine.NewVerifyEvidence(nil)
	require.NoError(t, err)

	for _, tier := range []finding.Tier{finding.TierE2, finding.TierE3, finding.TierE4} {
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
