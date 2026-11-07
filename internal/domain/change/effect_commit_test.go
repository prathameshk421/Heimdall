package change_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

func TestCommitTestsPayloadValidation(t *testing.T) {
	good := change.CommitTestsPayload{
		Repo:   change.Repo("acme/pay-demo"),
		Branch: "PAY-142-refunds",
		Dir:    "tests/heimdall/PAY-142",
		Files: []change.CommitFile{
			{
				Path:    "tests/heimdall/PAY-142/refund_test.go",
				BlobRef: workflow.BlobRef("blob:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
			},
		},
		Message: "tests: add test cases for PAY-142",
	}
	require.NoError(t, good.Validate())

	// Bad cases
	bad := good
	bad.Repo = ""
	require.Error(t, bad.Validate())

	bad = good
	bad.Branch = ""
	require.Error(t, bad.Validate())

	bad = good
	bad.Dir = "/abs/path"
	require.Error(t, bad.Validate())

	bad = good
	bad.Files = nil
	require.Error(t, bad.Validate())

	bad = good
	bad.Files = []change.CommitFile{
		{Path: "", BlobRef: workflow.BlobRef("blob:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")},
	}
	require.Error(t, bad.Validate())

	bad = good
	bad.Files = []change.CommitFile{
		{Path: "tests/foo.go", BlobRef: "not-a-blob"},
	}
	require.Error(t, bad.Validate())

	bad = good
	bad.Message = ""
	require.Error(t, bad.Validate())
}

func TestDecodeCommitTestsPayloadStrict(t *testing.T) {
	raw := json.RawMessage(`{
		"repo": "acme/pay-demo",
		"branch": "PAY-142-refunds",
		"dir": "tests/heimdall/PAY-142",
		"files": [
			{
				"path": "tests/heimdall/PAY-142/refund_test.go",
				"blob_ref": "blob:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			}
		],
		"message": "tests: add test cases for PAY-142"
	}`)
	p, err := change.DecodeCommitTestsPayload(raw)
	require.NoError(t, err)
	require.Equal(t, change.Repo("acme/pay-demo"), p.Repo)
	require.Equal(t, "PAY-142-refunds", p.Branch)
	require.Len(t, p.Files, 1)

	// Extra fields rejected
	extra := json.RawMessage(`{
		"repo": "acme/pay-demo",
		"branch": "PAY-142-refunds",
		"dir": "tests/heimdall/PAY-142",
		"files": [
			{
				"path": "tests/heimdall/PAY-142/refund_test.go",
				"blob_ref": "blob:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			}
		],
		"message": "tests: add test cases for PAY-142",
		"extra": true
	}`)
	_, err = change.DecodeCommitTestsPayload(extra)
	require.Error(t, err)
}
