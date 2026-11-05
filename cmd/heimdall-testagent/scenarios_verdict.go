package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/deliberation"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// caseFileSource names the untrusted input of a resolver's envelope that holds the case file.
const caseFileSource = "case_file"

// citationPlan is one citation a scripted verdict makes: the item it points at and the span it quotes.
type citationPlan struct {
	kind  deliberation.CitationKind
	ref   string
	quote string
}

// verdictPlan is one canned verdict. Each citation quote must be a span of the case file the server serves, so a case
// that does not hold the quote fails the run instead of sending a verdict that cites nothing.
type verdictPlan struct {
	decision   deliberation.Decision
	confidence finding.Confidence
	rationale  string
	citations  []citationPlan
}

// runVerdictUphold settles the disputed finding in favour of the reviewer: a failing run on head shows the behaviour.
func runVerdictUphold(ctx context.Context, c *client, out io.Writer) error {
	return verdictFlow(ctx, c, out, "verdict-uphold", upholdPlan())
}

// runVerdictDismiss settles the disputed finding in favour of the author: a confirmed decision answers the claim.
func runVerdictDismiss(ctx context.Context, c *client, out io.Writer) error {
	return verdictFlow(ctx, c, out, "verdict-dismiss", dismissPlan())
}

// runVerdictNeedsHuman escalates: the only support for one side is observed memory that no run can check.
func runVerdictNeedsHuman(ctx context.Context, c *client, out io.Writer) error {
	return verdictFlow(ctx, c, out, "verdict-needs-human", needsHumanPlan())
}

// upholdPlan is the canned uphold: the run and the confirmed invariant show that the refunds exceed the payment.
func upholdPlan() verdictPlan {
	return verdictPlan{
		decision:   deliberation.DecisionUphold,
		confidence: finding.ConfidenceHigh,
		rationale: "The second refund of 60 is accepted on head, so the refunds total 120 against a payment of 100. " +
			"The confirmed invariant says the sum may not exceed the payment. " +
			"A passing run of the same test on head would change this.",
		citations: []citationPlan{
			{kind: deliberation.CitationRun, ref: "run_01HQZX3K8V0M5N7P2R4S6T8WA0", quote: "--- FAIL: TestPartialRefundsCannotExceedPayment"},
			{kind: deliberation.CitationMemory, ref: "mem_refund_cap_invariant", quote: "the sum of the refunds of a payment never exceeds the payment"},
			{kind: deliberation.CitationFinding, ref: "F1", quote: "Two partial refunds of 60 on a payment of 100 both succeed"},
		},
	}
}

// dismissPlan is the canned dismissal: the confirmed ledger decision says the client reads reference, so the claim that
// a consumer breaks no longer holds.
func dismissPlan() verdictPlan {
	return verdictPlan{
		decision:   deliberation.DecisionDismiss,
		confidence: finding.ConfidenceHigh,
		rationale: "The ledger client reads reference since ledger v2, as the confirmed decision records. " +
			"Ledger v2 was deployed before this pull request, so the compatibility claim does not hold at head. " +
			"A consumer that still needs refund_ref would change this.",
		citations: []citationPlan{
			{kind: deliberation.CitationDecision, ref: "dec_2026_11_03_ledger_v2", quote: "since ledger v2 the client reads reference"},
			{kind: deliberation.CitationMemory, ref: "mem_ledger_v2_rollout", quote: "ledger v2 was deployed on 2026-11-03, before this pull request"},
			{kind: deliberation.CitationFinding, ref: "F2", quote: "a consumer that deserializes refund_ref as a required field breaks"},
		},
	}
}

// needsHumanPlan is the canned escalation: the run is on an older head and the author's memory is only observed.
func needsHumanPlan() verdictPlan {
	return verdictPlan{
		decision:   deliberation.DecisionNeedsHuman,
		confidence: finding.ConfidenceMedium,
		rationale: "The reviewer's run failed on an older head, and there is no run on the current head. " +
			"The author cites an observed memory about the gateway scoping reads, which the case does not confirm. " +
			"A fresh run on head, or a person, must decide.",
		citations: []citationPlan{
			{kind: deliberation.CitationRun, ref: "run_01HQZX3K8V0M5N7P2R4S6T8WB1", quote: "--- FAIL: TestRefundReadIsScopedToItsMerchant"},
			{kind: deliberation.CitationMemory, ref: "mem_gateway_scopes_reads", quote: "the gateway scopes refund reads by merchant since 2026-11-04"},
			{kind: deliberation.CitationFinding, ref: "F3", quote: "any merchant can read another merchant's refund"},
		},
	}
}

// verdictFlow plays the resolver flow: read the task, acknowledge it, read the case file and the disputed file at head,
// then submit the verdict. A refused get_task is the outcome, as for the reviewer.
func verdictFlow(ctx context.Context, c *client, out io.Writer, name string, plan verdictPlan) error {
	task, started, err := startTask(ctx, c, out, name)
	if err != nil || !started {
		return err
	}
	ref, err := caseFileRef(task)
	if err != nil {
		return err
	}
	caseRes, err := setupCall(ctx, c, toolReadUntrusted, map[string]any{"ref": string(ref)})
	if err != nil {
		return err
	}
	file, err := firstChanged(task)
	if err != nil {
		return err
	}
	if _, err := setupCall(ctx, c, toolReadCode, map[string]any{"path": file, "sha": string(task.Change.HeadSHA)}); err != nil {
		return err
	}
	v, err := plan.verdictFor(task, caseRes.text())
	if err != nil {
		return fmt.Errorf("build the %s verdict: %w", name, err)
	}
	res, err := c.callTool(ctx, toolSubmitVerdict, v)
	if err != nil {
		return err
	}
	return reportOutcome(out, name, res)
}

// caseFileRef returns the reference of the case file the envelope names.
func caseFileRef(task workflow.TaskEnvelope) (workflow.BlobRef, error) {
	for _, u := range task.Inputs.Untrusted {
		if u.Source == caseFileSource {
			return u.Ref, nil
		}
	}
	return "", errors.New("the task names no case file")
}

// caseIDOf returns the case the task settles. The server accepts a verdict only for the case that matches the task, so
// the case id is the task id with the case prefix.
func caseIDOf(task workflow.TaskEnvelope) id.CaseID {
	return id.CaseID("case_" + strings.TrimPrefix(string(task.TaskID), "tsk_"))
}

// verdictFor returns the verdict of the plan for the task. Each citation quote must be a span of the case text the agent
// read, or the plan does not match the case and nothing is sent.
func (p verdictPlan) verdictFor(task workflow.TaskEnvelope, caseText string) (deliberation.Verdict, error) {
	citations := make([]deliberation.Citation, 0, len(p.citations))
	for _, c := range p.citations {
		if !strings.Contains(caseText, c.quote) {
			return deliberation.Verdict{}, fmt.Errorf("the case file does not hold the quote for %s %s", c.kind, c.ref)
		}
		citations = append(citations, deliberation.Citation{Kind: c.kind, Ref: c.ref, Quote: c.quote})
	}
	return deliberation.Verdict{
		CaseID:     caseIDOf(task),
		Decision:   p.decision,
		Rationale:  p.rationale,
		Citations:  citations,
		Confidence: p.confidence,
	}, nil
}
