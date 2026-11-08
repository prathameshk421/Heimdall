package change

import (
	"errors"
	"fmt"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// ReviewersPicked is the payload of reviewers.picked: who was asked to review, and how they were chosen.
type ReviewersPicked struct {
	People []ActorID  `json:"people"`
	By     ActorID    `json:"by"`
	Method PickMethod `json:"method"`
}

// Validate returns every problem with the payload.
func (p ReviewersPicked) Validate() error {
	var v problems
	v.add("people", checkAtLeastOne(len(p.People)))
	v.each("people", p.People, checkActor)
	v.add("by", checkActor(p.By))
	v.add("method", checkEnum(p.Method, p.Method.Valid()))
	return v.join()
}

// ReviewSubmitted is the payload of review.submitted: a reviewer finished through the pack it was given (D156).
type ReviewSubmitted struct {
	Reviewer    id.AgentName    `json:"reviewer"`
	FindingRefs []id.FindingRef `json:"finding_refs"`
	PackDate    string          `json:"pack_date"`
	// RuleChecks is what the reviewer said about each rule card it was given. Old events have none.
	RuleChecks []finding.RuleCheck `json:"rule_checks,omitempty"`
}

// Validate returns every problem with the payload.
func (p ReviewSubmitted) Validate() error {
	var v problems
	v.add("reviewer", checkAgent(p.Reviewer))
	v.each("finding_refs", p.FindingRefs, checkFindingRef)
	v.add("pack_date", checkDate(p.PackDate))
	if len(p.RuleChecks) > finding.MaxRuleChecks {
		v.add("rule_checks", fmt.Errorf("must have at most %d entries", finding.MaxRuleChecks))
	}
	return v.join()
}

// ReviewUnavailable is the payload of review.unavailable: the reviewer did not answer in time (TECH_SPEC 6.2).
type ReviewUnavailable struct {
	Reviewer id.AgentName `json:"reviewer"`
	Reason   string       `json:"reason"`
}

// Validate returns every problem with the payload.
func (p ReviewUnavailable) Validate() error {
	var v problems
	v.add("reviewer", checkAgent(p.Reviewer))
	v.add("reason", checkText(p.Reason))
	return v.join()
}

// ReviewWaived is the payload of review.waived: an approver let the merge go ahead without this reviewer (D43).
type ReviewWaived struct {
	Reviewer id.AgentName `json:"reviewer"`
	Reason   string       `json:"reason"`
	Approver ActorID      `json:"approver"`
}

// Validate returns every problem with the payload.
func (p ReviewWaived) Validate() error {
	var v problems
	v.add("reviewer", checkAgent(p.Reviewer))
	v.add("reason", checkText(p.Reason))
	v.add("approver", checkActor(p.Approver))
	return v.join()
}

// FindingRecord is the content every finding event carries: the id, the short name people use, the tier, the state
// and who moved it (TECH_SPEC 3.2 and 11.1).
type FindingRecord struct {
	FindingID   id.FindingID       `json:"finding_id"`
	Ref         id.FindingRef      `json:"ref"`
	Tier        finding.Tier       `json:"tier"`
	Severity    finding.Severity   `json:"severity"`
	State       finding.State      `json:"state"`
	By          ActorID            `json:"by"`
	Fingerprint string             `json:"fingerprint,omitempty"`
	Confidence  finding.Confidence `json:"confidence,omitempty"`
	// ReproTest and ToolResult carry the evidence the sandbox repeats to verify the finding (E1 and E2). Only
	// finding.filed sets them.
	ReproTest  *finding.ReproTest  `json:"repro_test,omitempty"`
	ToolResult *finding.ToolResult `json:"tool_result,omitempty"`
	// MemoryRef is the memory reference of an E3 finding; the server re-queries it to verify the finding.
	MemoryRef *string `json:"memory_ref,omitempty"`
	// Claim, Category and Location are what people read about the finding. Only finding.filed sets them.
	Claim    string            `json:"claim,omitempty"`
	Category finding.Category  `json:"category,omitempty"`
	Location *finding.Location `json:"location,omitempty"`
	// CardRefs are the rule card files the reporter named in context_refs ("card:<file>"). Only finding.filed sets them.
	CardRefs []string `json:"card_refs,omitempty"`
}

// checkReproContent checks the source of a repro test. It is a whole test file, so it has the limit of the test source
// the sandbox accepts, not the limit of a short text field.
func checkReproContent(content string) error {
	switch {
	case strings.TrimSpace(content) == "":
		return errors.New("must not be blank")
	case len(content) > MaxTestSourceBytes:
		return fmt.Errorf("must be at most %d bytes", MaxTestSourceBytes)
	}
	return nil
}

// Validate returns every problem with the record.
func (p FindingRecord) Validate() error {
	var v problems
	_, err := id.ParseFindingID(string(p.FindingID))
	v.add("finding_id", err)
	v.add("ref", checkFindingRef(p.Ref))
	_, err = finding.ParseTier(string(p.Tier))
	v.add("tier", err)
	_, err = finding.ParseSeverity(string(p.Severity))
	v.add("severity", err)
	_, err = finding.ParseState(string(p.State))
	v.add("state", err)
	v.add("by", checkActor(p.By))
	if p.Fingerprint != "" {
		v.add("fingerprint", checkText(p.Fingerprint))
	}
	if p.Confidence != "" {
		_, err := finding.ParseConfidence(string(p.Confidence))
		v.add("confidence", err)
	}
	if p.ReproTest != nil {
		v.add("repro_test.path", finding.CheckRepoPath(p.ReproTest.Path))
		v.add("repro_test.name", checkText(p.ReproTest.Name))
		v.add("repro_test.content", checkReproContent(p.ReproTest.Content))
	}
	if p.MemoryRef != nil {
		v.add("memory_ref", checkText(*p.MemoryRef))
	}
	if p.ToolResult != nil {
		v.add("tool_result.tool", checkText(p.ToolResult.Tool))
		v.add("tool_result.location.file", finding.CheckRepoPath(p.ToolResult.Location.File))
		v.require("tool_result.location.lines", p.ToolResult.Location.Lines[0] >= 1 && p.ToolResult.Location.Lines[1] >= p.ToolResult.Location.Lines[0], "must be 1-based with the first at most the last")
	}
	return v.join()
}

// FindingFiled is the payload of finding.filed.
type FindingFiled FindingRecord

// Validate returns every problem with the payload.
func (p FindingFiled) Validate() error { return FindingRecord(p).Validate() }

// FindingVerified is the payload of finding.verified: a re-run or a verification confirmed the finding's evidence.
type FindingVerified FindingRecord

// Validate returns every problem with the payload.
func (p FindingVerified) Validate() error { return FindingRecord(p).Validate() }

// FindingStateChanged is the payload of finding.state_changed (the lifecycle of TECH_SPEC 11.2).
type FindingStateChanged FindingRecord

// Validate returns every problem with the payload.
func (p FindingStateChanged) Validate() error { return FindingRecord(p).Validate() }

// FindingRecheckRequested is the payload of finding.recheck_requested: a person asked the server to repeat the
// evidence of one finding on the current head. The next verification result replaces the earlier one.
type FindingRecheckRequested struct {
	FindingID id.FindingID  `json:"finding_id"`
	Ref       id.FindingRef `json:"ref"`
	Reason    string        `json:"reason"`
	By        ActorID       `json:"by"`
}

// Validate returns every problem with the payload.
func (p FindingRecheckRequested) Validate() error {
	var v problems
	_, err := id.ParseFindingID(string(p.FindingID))
	v.add("finding_id", err)
	v.add("ref", checkFindingRef(p.Ref))
	v.add("reason", checkText(p.Reason))
	v.add("by", checkActor(p.By))
	return v.join()
}

// checkFindingRef requires a finding name such as F3 (a number from 1).
func checkFindingRef(r id.FindingRef) error {
	_, err := id.ParseFindingRef(r.String())
	return err
}

// ChallengeFiled is the payload of challenge.filed: an author challenged a finding with evidence (D152).
// Round is 1 or 2, the limit of TECH_SPEC 11.3.
type ChallengeFiled struct {
	Finding  id.FindingRef `json:"finding"`
	Round    int           `json:"round"`
	Evidence string        `json:"evidence"`
}

// Validate returns every problem with the payload.
func (p ChallengeFiled) Validate() error {
	var v problems
	v.add("finding", checkFindingRef(p.Finding))
	if p.Round < 1 || p.Round > 2 {
		v.add("round", errors.New("must be 1 or 2"))
	}
	v.add("evidence", checkText(p.Evidence))
	return v.join()
}

// CaseOpened is the payload of case.opened: a finding went to deliberation (TECH_SPEC 11.3).
type CaseOpened struct {
	CaseID  id.CaseID     `json:"case_id"`
	Finding id.FindingRef `json:"finding"`
}

// Validate returns every problem with the payload.
func (p CaseOpened) Validate() error {
	var v problems
	v.add("case_id", checkCase(p.CaseID))
	v.add("finding", checkFindingRef(p.Finding))
	return v.join()
}

// checkCase requires a case id (case_ and a ULID).
func checkCase(c id.CaseID) error {
	_, err := id.ParseCaseID(string(c))
	return err
}

// VerdictSubmitted is the payload of verdict.submitted: the resolver answered a case. Citations and checks are
// what a later verdict.applied must re-verify (invariant 6).
type VerdictSubmitted struct {
	CaseID    id.CaseID `json:"case_id"`
	Verdict   Verdict   `json:"verdict"`
	Citations []string  `json:"citations"`
	Checks    []string  `json:"checks"`
}

// Validate returns every problem with the payload.
func (p VerdictSubmitted) Validate() error {
	var v problems
	v.add("case_id", checkCase(p.CaseID))
	v.add("verdict", checkEnum(p.Verdict, p.Verdict.Valid()))
	v.each("citations", p.Citations, checkText)
	v.each("checks", p.Checks, checkText)
	return v.join()
}

// VerdictApplied is the payload of verdict.applied. It needs at least one citation and one re-verification check,
// because a verdict is applied only on citations re-verified in the same transaction (invariant 6).
type VerdictApplied VerdictSubmitted

// Validate returns every problem with the payload.
func (p VerdictApplied) Validate() error {
	var v problems
	v.add("case_id", checkCase(p.CaseID))
	v.add("verdict", checkEnum(p.Verdict, p.Verdict.Valid()))
	v.add("citations", checkAtLeastOne(len(p.Citations)))
	v.each("citations", p.Citations, checkText)
	v.add("checks", checkAtLeastOne(len(p.Checks)))
	v.each("checks", p.Checks, checkText)
	return v.join()
}

// VerdictEscalated is the payload of verdict.escalated: the case goes to a human (TECH_SPEC 11.3, step 4).
type VerdictEscalated struct {
	CaseID id.CaseID `json:"case_id"`
	Reason string    `json:"reason"`
}

// Validate returns every problem with the payload.
func (p VerdictEscalated) Validate() error {
	var v problems
	v.add("case_id", checkCase(p.CaseID))
	v.add("reason", checkText(p.Reason))
	return v.join()
}

// DecisionRecorded is the payload of decision.recorded: a human decision from GitHub or from the room (TECH_SPEC 11.5).
// Only an actor that is not an agent may cause it (invariant 1).
type DecisionRecorded struct {
	Kind      DecisionKind `json:"kind"`
	Target    string       `json:"target"`
	Login     ActorID      `json:"login"`
	Reason    string       `json:"reason"`
	SourceRef string       `json:"source_ref"`
}

// Validate returns every problem with the payload.
func (p DecisionRecorded) Validate() error {
	var v problems
	v.add("kind", checkEnum(p.Kind, p.Kind.Valid()))
	v.add("target", checkText(p.Target))
	v.add("login", checkActor(p.Login))
	v.add("reason", checkText(p.Reason))
	v.add("source_ref", checkText(p.SourceRef))
	return v.join()
}

// ApprovalRecorded is the payload of approval.recorded: a GitHub review approved a commit (TECH_SPEC 3.4, 12.4).
type ApprovalRecorded struct {
	Login     ActorID `json:"login"`
	CommitSHA id.SHA  `json:"commit_sha"`
}

// Validate returns every problem with the payload.
func (p ApprovalRecorded) Validate() error {
	var v problems
	v.add("login", checkActor(p.Login))
	v.add("commit_sha", checkSHA(p.CommitSHA))
	return v.join()
}

// ApprovalInvalidated is the payload of approval.invalidated: a push made an earlier approval stale.
type ApprovalInvalidated ApprovalRecorded

// Validate returns every problem with the payload.
func (p ApprovalInvalidated) Validate() error { return ApprovalRecorded(p).Validate() }
