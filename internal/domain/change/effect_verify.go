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
	Repo       Repo             `json:"repo"`
	Ticket     id.ChangeID      `json:"ticket"`
	FindingID  id.FindingID     `json:"finding_id"`
	FindingRef id.FindingRef    `json:"finding_ref"`
	Severity   finding.Severity `json:"severity"`
	HeadSHA    id.SHA           `json:"head_sha"`
	TestPath   string           `json:"test_path,omitempty"`
	TestSource string           `json:"test_source,omitempty"`
	TestName   string           `json:"test_name,omitempty"`
	// Check, when set, makes this an E2 verification: a fixed static check re-run on the head. The repro test fields
	// stay empty then.
	Check *VerifyCheck `json:"check,omitempty"`
	// Memory, when set, makes this an E3 verification: the cited memory reference is looked up again.
	Memory *VerifyMemory `json:"memory,omitempty"`
}

// VerifyMemory names the memory reference an E3 verification re-queries and the agent whose scopes the query uses.
type VerifyMemory struct {
	Ref   string `json:"ref"`
	Agent string `json:"agent"`
}

// Validate returns every problem with the memory check.
func (m VerifyMemory) Validate() error {
	var v problems
	v.add("ref", checkText(m.Ref))
	v.add("agent", checkText(m.Agent))
	return v.join()
}

// VerifyCheck names the static check an E2 verification re-runs and the location the finding is about.
type VerifyCheck struct {
	Name  string `json:"name"`
	File  string `json:"file"`
	Lines [2]int `json:"lines"`
}

// CheckGoVet is the name of the go vet check (ports.CheckGoVet).
const CheckGoVet = "go_vet"

// Validate returns every problem with the check.
func (c VerifyCheck) Validate() error {
	var v problems
	v.require("name", c.Name == CheckGoVet, "must be a known check")
	v.add("file", finding.CheckRepoPath(c.File))
	v.require("lines", c.Lines[0] >= 1 && c.Lines[1] >= c.Lines[0], "must be 1-based with the first at most the last")
	return v.join()
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
	_, err = finding.ParseSeverity(string(p.Severity))
	v.add("severity", err)
	v.add("head_sha", checkSHA(p.HeadSHA))
	if p.Check != nil && p.Memory != nil {
		v.require("memory", false, "a verification is a check or a memory re-query, not both")
		return v.join()
	}
	if p.Memory != nil {
		v.add("memory", p.Memory.Validate())
		v.require("test_path", p.TestPath == "" && p.TestSource == "" && p.TestName == "", "a memory verification carries no repro test")
		return v.join()
	}
	if p.Check != nil {
		v.add("check", p.Check.Validate())
		v.require("test_path", p.TestPath == "" && p.TestSource == "" && p.TestName == "", "a check verification carries no repro test")
		return v.join()
	}
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
	fields := []string{"repo", "ticket", "finding_id", "finding_ref", "head_sha"}
	// A check verification (E2) names a check; any other payload names a repro test (E1).
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err == nil && probe["check"] != nil {
		fields = append(fields, "check")
	} else if err == nil && probe["memory"] != nil {
		fields = append(fields, "memory")
	} else {
		fields = append(fields, "test_path", "test_source", "test_name")
	}
	if err := decodeEffectPayload(raw, fields, &p); err != nil {
		return VerifyEvidencePayload{}, fmt.Errorf("verify_evidence payload: %w", err)
	}
	return p, nil
}
