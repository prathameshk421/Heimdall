package change_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

func TestTestQualityPayloadValidation(t *testing.T) {
	good := change.TestQualityPayload{
		Repo:          change.Repo("acme/pay-demo"),
		Branch:        "PAY-142-refunds",
		Ticket:        id.ChangeID("PAY-142"),
		SubmissionRef: workflow.BlobRef("blob:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
	}
	require.NoError(t, good.Validate())

	bad := good
	bad.Repo = ""
	require.Error(t, bad.Validate())

	bad = good
	bad.Branch = ""
	require.Error(t, bad.Validate())

	bad = good
	bad.Ticket = ""
	require.Error(t, bad.Validate())

	bad = good
	bad.SubmissionRef = "invalid-blob"
	require.Error(t, bad.Validate())
}

func TestDecodeTestQualityPayloadStrict(t *testing.T) {
	raw := json.RawMessage(`{
		"repo": "acme/pay-demo",
		"branch": "PAY-142-refunds",
		"ticket": "PAY-142",
		"submission_ref": "blob:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	}`)
	p, err := change.DecodeTestQualityPayload(raw)
	require.NoError(t, err)
	require.Equal(t, change.Repo("acme/pay-demo"), p.Repo)
	require.Equal(t, "PAY-142-refunds", p.Branch)
	require.Equal(t, id.ChangeID("PAY-142"), p.Ticket)
	require.Equal(t, workflow.BlobRef("blob:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), p.SubmissionRef)

	// Extra fields rejected
	extra := json.RawMessage(`{
		"repo": "acme/pay-demo",
		"branch": "PAY-142-refunds",
		"ticket": "PAY-142",
		"submission_ref": "blob:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"extra": true
	}`)
	_, err = change.DecodeTestQualityPayload(extra)
	require.Error(t, err)
}
