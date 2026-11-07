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

func TestCommitTestsStepContract(t *testing.T) {
	with := json.RawMessage(`{"dir":"tests/heimdall/{ticket}"}`)
	step, err := engine.NewCommitTests(with)
	require.NoError(t, err)
	require.Equal(t, "github.commit_tests", step.Type())

	ticket := id.ChangeID("PAY-142")
	blobRef := workflow.BlobRef("blob:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

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
			{
				ID:       "ev-3",
				ChangeID: ticket,
				Type:     change.EventTestsSubmitted,
				BlobRefs: []workflow.BlobRef{blobRef},
				Payload:  json.RawMessage(`{"refs":["tests/heimdall/PAY-142/refund_test.go"],"ac_map":{"AC1":["TestA"]},"sha":"0123456789abcdef0123456789abcdef01234567","tree_hash":"0123456789abcdef0123456789abcdef01234567"}`),
			},
		},
	}
	require.False(t, step.Done(view))

	// Start does nothing
	res, err := step.Start(context.Background(), view, with)
	require.NoError(t, err)
	require.Empty(t, res.Events)
	require.Empty(t, res.Effects)

	// tests.verified with needs_signoff=true does NOT return effect
	verifiedNeedsSignoff := change.Event{
		ID:       "ev-4",
		Version:  4,
		ChangeID: ticket,
		Type:     change.EventTestsVerified,
		Payload:  json.RawMessage(`{"run_ids":["run_01ARZ3NDEKTSV4RRFFQ69G5FAV"],"sha":"0123456789abcdef0123456789abcdef01234567","tree_hash":"0123456789abcdef0123456789abcdef01234567","needs_signoff":true}`),
	}
	res, err = step.OnEvent(context.Background(), view, verifiedNeedsSignoff)
	require.NoError(t, err)
	require.Empty(t, res.Effects)

	// tests.approved after signoff returns the effect
	approvedEv := change.Event{
		ID:       "ev-5",
		Version:  5,
		ChangeID: ticket,
		Type:     change.EventTestsApproved,
		Payload:  json.RawMessage(`{"approver":"ana","tree_hash":"0123456789abcdef0123456789abcdef01234567"}`),
	}
	res, err = step.OnEvent(context.Background(), view, approvedEv)
	require.NoError(t, err)
	require.Len(t, res.Effects, 1)

	fx := res.Effects[0]
	require.Equal(t, change.EffectGitHubCommitTests, fx.Kind)

	var p change.CommitTestsPayload
	require.NoError(t, json.Unmarshal(fx.Payload, &p))
	require.Equal(t, change.Repo("acme/pay-demo"), p.Repo)
	require.Equal(t, "PAY-142-refunds", p.Branch)
	require.Equal(t, "tests/heimdall/PAY-142", p.Dir)
	require.Len(t, p.Files, 1)
	require.Equal(t, "tests/heimdall/PAY-142/refund_test.go", p.Files[0].Path)
	require.Equal(t, blobRef, p.Files[0].BlobRef)

	// tests.verified without signoff returns the effect directly
	verifiedNoSignoff := change.Event{
		ID:       "ev-4b",
		Version:  4,
		ChangeID: ticket,
		Type:     change.EventTestsVerified,
		Payload:  json.RawMessage(`{"run_ids":["run_01ARZ3NDEKTSV4RRFFQ69G5FAV"],"sha":"0123456789abcdef0123456789abcdef01234567","tree_hash":"0123456789abcdef0123456789abcdef01234567","needs_signoff":false}`),
	}
	res, err = step.OnEvent(context.Background(), view, verifiedNoSignoff)
	require.NoError(t, err)
	require.Len(t, res.Effects, 1)

	// Once tests.committed is in the view, Done is true and OnEvent returns nothing
	view.Events = append(view.Events, change.Event{
		ID:       "ev-6",
		ChangeID: ticket,
		Type:     change.EventTestsCommitted,
	})
	require.True(t, step.Done(view))

	res, err = step.OnEvent(context.Background(), view, approvedEv)
	require.NoError(t, err)
	require.Empty(t, res.Effects)
}
