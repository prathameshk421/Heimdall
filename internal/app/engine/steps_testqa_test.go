package engine_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/engine"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

func TestTestQAStepContract(t *testing.T) {
	with := json.RawMessage(`{"checks":["red_first","flake"]}`)
	qaStep, err := engine.NewTestQA(with)
	require.NoError(t, err)
	require.Equal(t, "sandbox.test_quality", qaStep.Type())

	blobRef := workflow.BlobRef("blob:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	ticket := id.ChangeID("PAY-142")

	view := engine.ChangeView{
		ID: ticket,
		Events: []change.Event{
			{
				ID:       "ev-1",
				ChangeID: ticket,
				Type:     change.EventChangeTriggered,
				Payload:  json.RawMessage(`{"ticket":"PAY-142","repo":"acme/pay-demo","assignee":"ana","source":"jira"}`),
			},
			{
				ID:       "ev-2",
				ChangeID: ticket,
				Type:     change.EventBranchEnsured,
				Payload:  json.RawMessage(`{"repo":"acme/pay-demo","branch":"PAY-142-refunds","base_sha":"0123456789abcdef0123456789abcdef01234567"}`),
			},
		},
	}
	require.False(t, qaStep.Done(view))

	// Start does nothing
	res, err := qaStep.Start(context.Background(), view, with)
	require.NoError(t, err)
	require.Empty(t, res.Events)
	require.Empty(t, res.Effects)

	// OnEvent(tests.submitted) returns the test_quality effect
	submitEv := change.Event{
		ID:            "ev-3",
		Version:       3,
		ChangeID:      ticket,
		Type:          change.EventTestsSubmitted,
		BlobRefs:      []workflow.BlobRef{blobRef},
		Payload:       json.RawMessage(`{"refs":["tests/a_test.go"],"ac_map":{"AC1":["TestA"]},"sha":"0123456789abcdef0123456789abcdef01234567","tree_hash":"0123456789abcdef0123456789abcdef01234567"}`),
		CorrelationID: "corr-1",
	}

	res, err = qaStep.OnEvent(context.Background(), view, submitEv)
	require.NoError(t, err)
	require.Empty(t, res.Events, "effect attaches to incoming event, no event returned")
	require.Len(t, res.Effects, 1)

	fx := res.Effects[0]
	require.Equal(t, change.EffectTestQuality, fx.Kind)
	require.Equal(t, string(ticket), fx.Target)

	var p change.TestQualityPayload
	require.NoError(t, json.Unmarshal(fx.Payload, &p))
	require.Equal(t, change.Repo("acme/pay-demo"), p.Repo)
	require.Equal(t, "PAY-142-refunds", p.Branch)
	require.Equal(t, ticket, p.Ticket)
	require.Equal(t, blobRef, p.SubmissionRef)

	// OnEvent(tests.rejected) returns StepFailedError
	rejectEv := change.Event{
		ID:       "ev-4",
		ChangeID: ticket,
		Type:     change.EventTestsRejected,
		Payload:  json.RawMessage(`{"reasons":["AC1 has no test"],"run_ids":["run_01ARZ3NDEKTSV4RRFFQ69G5FAV"],"sha":"0123456789abcdef0123456789abcdef01234567"}`),
	}
	_, err = qaStep.OnEvent(context.Background(), view, rejectEv)
	require.Error(t, err)
	var sf engine.StepFailedError
	require.ErrorAs(t, err, &sf)
	require.Equal(t, []string{"AC1 has no test"}, sf.Reasons)

	// When tests.verified is in the view, Done is true
	view.Events = append(view.Events, change.Event{
		ID:       "ev-5",
		ChangeID: ticket,
		Type:     change.EventTestsVerified,
	})
	require.True(t, qaStep.Done(view))
}
