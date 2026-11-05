package change

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// MaxTestSourceBytes is the upper bound on the decoded repro test source size (64 KiB).
const (
	MaxTestSourceBytes = 64 * 1024
	maxTestSourceChars = ((MaxTestSourceBytes + 2) / 3) * 4
)

// VerifyEvidencePayload is the payload of a verify_evidence effect (TECH_SPEC 11.1).
type VerifyEvidencePayload struct {
	Repo       Repo          `json:"repo"`
	Ticket     id.ChangeID   `json:"ticket"`
	FindingID  id.FindingID  `json:"finding_id"`
	FindingRef id.FindingRef `json:"finding_ref"`
	HeadSHA    id.SHA        `json:"head_sha"`
	TestPath   string        `json:"test_path"`
	TestSource string        `json:"test_source"`
	TestName   string        `json:"test_name"`
}

// Validate returns every problem with the payload.
func (p VerifyEvidencePayload) Validate() error {
	var v problems
	v.add("repo", checkRepo(p.Repo))
	_, err := id.ParseChangeID(string(p.Ticket))
	v.add("ticket", err)
	_, err = id.ParseFindingID(string(p.FindingID))
	v.add("finding_id", err)
	v.add("finding_ref", checkFindingRef(p.FindingRef))
	v.add("head_sha", checkSHA(p.HeadSHA))
	v.add("test_path", finding.CheckRepoPath(p.TestPath))
	v.add("test_source", checkTestSource(p.TestSource))
	v.add("test_name", checkText(p.TestName))
	return v.join()
}

func isValidBase64Char(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '+' || c == '/'
}

func checkTestSource(src string) error {
	trimmed := strings.TrimSpace(src)
	if trimmed == "" {
		return errors.New("must not be blank")
	}
	if len(trimmed)%4 != 0 {
		return errors.New("length must be a multiple of 4")
	}
	if len(trimmed) > maxTestSourceChars {
		return fmt.Errorf("length %d exceeds maximum of %d characters", len(trimmed), maxTestSourceChars)
	}
	for i := 0; i < len(trimmed); i++ {
		c := trimmed[i]
		if isValidBase64Char(c) {
			continue
		}
		if c == '=' && i >= len(trimmed)-2 {
			continue
		}
		return fmt.Errorf("invalid base64 character at %d: %q", i, c)
	}
	decodedBytes := len(trimmed) / 4 * 3
	if strings.HasSuffix(trimmed, "=") {
		decodedBytes--
	}
	if strings.HasSuffix(trimmed, "==") {
		decodedBytes--
	}
	if decodedBytes > MaxTestSourceBytes {
		return fmt.Errorf("decoded length %d exceeds maximum of %d bytes", decodedBytes, MaxTestSourceBytes)
	}
	return nil
}

// DecodeVerifyEvidencePayload strictly decodes and validates a verify_evidence payload.
func DecodeVerifyEvidencePayload(raw json.RawMessage) (VerifyEvidencePayload, error) {
	var p VerifyEvidencePayload
	fields := []string{"repo", "ticket", "finding_id", "finding_ref", "head_sha", "test_path", "test_source", "test_name"}
	if err := decodeEffectPayload(raw, fields, &p); err != nil {
		return VerifyEvidencePayload{}, fmt.Errorf("verify_evidence payload: %w", err)
	}
	return p, nil
}
