package change_test

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

func validVerifyEvidencePayload() change.VerifyEvidencePayload {
	src := base64.StdEncoding.EncodeToString([]byte("package test\nfunc TestRepro(t *testing.T){}\n"))
	ref, err := id.ParseFindingRef("F1")
	if err != nil {
		panic(err)
	}
	return change.VerifyEvidencePayload{
		Repo:       change.Repo("acme/pay-demo"),
		Ticket:     id.ChangeID("PAY-142"),
		FindingID:  id.FindingID("fnd_01HZX3K9QW7M2P5R8T0V4YB6CD"),
		FindingRef: ref,
		Severity:   finding.SeverityHigh,
		HeadSHA:    id.SHA("0123456789abcdef0123456789abcdef01234567"),
		TestPath:   "tests/heimdall/repro/repro_test.go",
		TestSource: src,
		TestName:   "TestRepro",
	}
}

func TestVerifyEvidencePayloadValidation(t *testing.T) {
	good := validVerifyEvidencePayload()
	require.NoError(t, good.Validate())

	bad := good
	bad.Repo = ""
	require.Error(t, bad.Validate())

	bad = good
	bad.Ticket = ""
	require.Error(t, bad.Validate())

	bad = good
	bad.FindingID = "bad-id"
	require.Error(t, bad.Validate())

	bad = good
	bad.FindingRef = id.FindingRef(0)
	require.Error(t, bad.Validate())

	bad = good
	bad.HeadSHA = "short"
	require.Error(t, bad.Validate())

	bad = good
	bad.TestPath = "../outside.go"
	require.Error(t, bad.Validate())

	bad = good
	bad.TestPath = ""
	require.Error(t, bad.Validate())

	bad = good
	bad.TestSource = "not base64!"
	require.Error(t, bad.Validate())

	bad = good
	bad.TestSource = ""
	require.Error(t, bad.Validate())

	bad = good
	bad.TestSource = base64.StdEncoding.EncodeToString([]byte{})
	require.Error(t, bad.Validate())

	// TestSource exceeding max bytes
	huge := make([]byte, change.MaxTestSourceBytes+1)
	bad = good
	bad.TestSource = base64.StdEncoding.EncodeToString(huge)
	require.Error(t, bad.Validate())

	bad = good
	bad.TestName = ""
	require.Error(t, bad.Validate())

	bad = good
	bad.TestName = "   "
	require.Error(t, bad.Validate())
}

func TestDecodeVerifyEvidencePayloadStrict(t *testing.T) {
	ref, err := id.ParseFindingRef("F1")
	require.NoError(t, err)

	src := base64.StdEncoding.EncodeToString([]byte("package test\n"))
	raw := json.RawMessage(`{
		"repo": "acme/pay-demo",
		"ticket": "PAY-142",
		"finding_id": "fnd_01HZX3K9QW7M2P5R8T0V4YB6CD",
		"finding_ref": "F1",
		"severity": "high",
		"head_sha": "0123456789abcdef0123456789abcdef01234567",
		"test_path": "tests/repro_test.go",
		"test_source": "` + src + `",
		"test_name": "TestRepro"
	}`)
	p, err := change.DecodeVerifyEvidencePayload(raw)
	require.NoError(t, err)
	require.Equal(t, change.Repo("acme/pay-demo"), p.Repo)
	require.Equal(t, id.ChangeID("PAY-142"), p.Ticket)
	require.Equal(t, id.FindingID("fnd_01HZX3K9QW7M2P5R8T0V4YB6CD"), p.FindingID)
	require.Equal(t, ref, p.FindingRef)
	require.Equal(t, id.SHA("0123456789abcdef0123456789abcdef01234567"), p.HeadSHA)
	require.Equal(t, "tests/repro_test.go", p.TestPath)
	require.Equal(t, src, p.TestSource)
	require.Equal(t, "TestRepro", p.TestName)

	// Extra fields rejected
	extra := json.RawMessage(`{
		"repo": "acme/pay-demo",
		"ticket": "PAY-142",
		"finding_id": "fnd_01HZX3K9QW7M2P5R8T0V4YB6CD",
		"finding_ref": "F1",
		"severity": "high",
		"head_sha": "0123456789abcdef0123456789abcdef01234567",
		"test_path": "tests/repro_test.go",
		"test_source": "` + src + `",
		"test_name": "TestRepro",
		"extra": "boom"
	}`)
	_, err = change.DecodeVerifyEvidencePayload(extra)
	require.Error(t, err)

	// Missing field rejected
	missing := json.RawMessage(`{
		"repo": "acme/pay-demo",
		"ticket": "PAY-142"
	}`)
	_, err = change.DecodeVerifyEvidencePayload(missing)
	require.Error(t, err)
}

func TestEffectKindVerifyEvidence(t *testing.T) {
	require.True(t, change.EffectVerifyEvidence.Valid())
	require.False(t, change.EffectKind("other").Valid())

	key, err := change.IdempotencyKey(
		id.ChangeID("PAY-142"),
		1,
		change.EffectVerifyEvidence,
		"fnd_01HZX3K9QW7M2P5R8T0V4YB6CD:0123456789abcdef0123456789abcdef01234567",
	)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(key), "verify_evidence:PAY-142:1:"))
}
