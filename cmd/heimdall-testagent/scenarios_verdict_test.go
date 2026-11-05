package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/deliberation"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// caseRef is the blob reference of the case file the resolver's envelope names.
const caseRef = workflow.BlobRef("blob:sha256:4444444444444444444444444444444444444444444444444444444444444444")

// The case files the tests serve. Each holds every quote its scenario cites, and nothing the scenario does not cite is
// needed. The text is what read_untrusted returns for the case ref.
const (
	upholdCase = "Case case_01J8Z3K9QW7M2P5R8T0V4YB6CD. Disputed finding F1, reviewer, correctness, high.\n" +
		"F1: Two partial refunds of 60 on a payment of 100 both succeed, so the merchant refunds 120.\n" +
		"Run run_01HQZX3K8V0M5N7P2R4S6T8WA0 on head 9f1c2ab: --- FAIL: TestPartialRefundsCannotExceedPayment\n" +
		"Memory mem_refund_cap_invariant (confirmed): the sum of the refunds of a payment never exceeds the payment.\n"

	dismissCase = "Case case_01J8Z3K9QW7M2P5R8T0V4YB6CD. Disputed finding F2, reviewer, compat, high.\n" +
		"F2: a consumer that deserializes refund_ref as a required field breaks on the new response.\n" +
		"Decision dec_2026_11_03_ledger_v2 (confirmed): since ledger v2 the client reads reference.\n" +
		"Memory mem_ledger_v2_rollout (confirmed): ledger v2 was deployed on 2026-11-03, before this pull request.\n"

	needsHumanCase = "Case case_01J8Z3K9QW7M2P5R8T0V4YB6CD. Disputed finding F3, reviewer, security, critical.\n" +
		"F3: any merchant can read another merchant's refund.\n" +
		"Run run_01HQZX3K8V0M5N7P2R4S6T8WB1 on an older head: --- FAIL: TestRefundReadIsScopedToItsMerchant\n" +
		"Memory mem_gateway_scopes_reads (observed): the gateway scopes refund reads by merchant since 2026-11-04.\n"
)

// resolverTask is the envelope a resolver gets for the fixed case. The case file is its one untrusted input, and the
// disputed file is the envelope's first changed file.
func resolverTask(untrusted ...workflow.UntrustedRef) workflow.TaskEnvelope {
	task := testTask()
	task.Role = workflow.RoleResolver
	task.Skill = "resolver@1.0.0"
	task.OutputSchema = "Verdict@1"
	task.Inputs.FilesChanged = []string{"internal/refunds/service.go"}
	task.Inputs.Untrusted = untrusted
	return task
}

// caseEnvelope is the resolver's envelope with its case file named.
func caseEnvelope() workflow.TaskEnvelope {
	return resolverTask(workflow.UntrustedRef{Source: "case_file", Ref: caseRef})
}

// playVerdictScenario runs one scenario against a world and returns the exit status and the two output streams.
func playVerdictScenario(t *testing.T, w world, name string) (int, string, string) {
	t.Helper()
	return runScenario(t, w.hs.URL+"/mcp", writeToken(t), name)
}

// verdictWorld serves the resolver envelope with the given case text as the case file.
func verdictWorld(t *testing.T, caseText string) world {
	t.Helper()
	return buildWorld(t, workflow.RoleResolver, caseEnvelope(), &testBlobs{texts: map[workflow.BlobRef]string{caseRef: caseText}})
}

// TestTheVerdictScenariosRecordAVerdictOnTheRealServer plays the three verdicts against the real MCP edge. Each verdict
// is for the case the envelope names, validates, and quotes only text the case file holds.
func TestTheVerdictScenariosRecordAVerdictOnTheRealServer(t *testing.T) {
	cases := []struct {
		name       string
		caseText   string
		decision   deliberation.Decision
		confidence finding.Confidence
	}{
		{name: "verdict-uphold", caseText: upholdCase, decision: deliberation.DecisionUphold, confidence: finding.ConfidenceHigh},
		{name: "verdict-dismiss", caseText: dismissCase, decision: deliberation.DecisionDismiss, confidence: finding.ConfidenceHigh},
		{name: "verdict-needs-human", caseText: needsHumanCase, decision: deliberation.DecisionNeedsHuman, confidence: finding.ConfidenceMedium},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := verdictWorld(t, c.caseText)
			code, stdout, stderr := playVerdictScenario(t, w, c.name)
			require.Equal(t, exitOK, code, "stderr: %s", stderr)
			require.Equal(t, c.name+": recorded: The verdict is recorded.", strings.TrimSpace(stdout))
			require.Len(t, w.reviews.verdicts, 1)
			v := w.reviews.verdicts[0]
			require.Equal(t, id.CaseID("case_01J8Z3K9QW7M2P5R8T0V4YB6CD"), v.CaseID, "the verdict settles the case of the task")
			require.Equal(t, c.decision, v.Decision)
			require.Equal(t, c.confidence, v.Confidence)
			require.NoError(t, v.Validate(), "the scripted verdict passes the domain's own rules")
			require.Contains(t, w.blobs.reads, caseRef, "the resolver reads the case file before it decides")
			require.Contains(t, w.code.reads, string(testHead)+" internal/refunds/service.go",
				"the resolver reads the disputed file at head")
			for _, cit := range v.Citations {
				require.Contains(t, c.caseText, cit.Quote, "citation %s %s quotes text the case does not hold", cit.Kind, cit.Ref)
			}
		})
	}
}

// TestAVerdictScenarioRefusesAQuoteTheCaseDoesNotHold is the guard on the scripted citations: a case that does not hold
// the quote is a wrong setup, so nothing is submitted and the run fails.
func TestAVerdictScenarioRefusesAQuoteTheCaseDoesNotHold(t *testing.T) {
	w := verdictWorld(t, "Case case_01J8Z3K9QW7M2P5R8T0V4YB6CD. Nothing in this case is about refunds.\n")
	code, stdout, stderr := playVerdictScenario(t, w, "verdict-uphold")
	require.Equal(t, exitFailure, code)
	require.Contains(t, stderr, "does not hold")
	require.Empty(t, stdout)
	require.Empty(t, w.reviews.verdicts, "a verdict with a quote the case does not hold must not be sent")
}

// TestAVerdictScenarioNeedsTheCaseFileInItsEnvelope fails loudly when the task names no case file.
func TestAVerdictScenarioNeedsTheCaseFileInItsEnvelope(t *testing.T) {
	w := buildWorld(t, workflow.RoleResolver, resolverTask(), &testBlobs{})
	code, _, stderr := playVerdictScenario(t, w, "verdict-dismiss")
	require.Equal(t, exitFailure, code)
	require.Contains(t, stderr, "case")
	require.Empty(t, w.reviews.verdicts)
}

// TestAVerdictScenarioWithAReviewerTokenIsRefused is the role mismatch for a resolver: the refusal is the outcome and
// the run exits 0.
func TestAVerdictScenarioWithAReviewerTokenIsRefused(t *testing.T) {
	w := buildWorld(t, workflow.RoleReviewer, caseEnvelope(), &testBlobs{texts: map[workflow.BlobRef]string{caseRef: upholdCase}})
	code, stdout, stderr := playVerdictScenario(t, w, "verdict-uphold")
	require.Equal(t, exitOK, code, "stderr: %s", stderr)
	require.Equal(t, "verdict-uphold: refused: Your active task is not for your role.", strings.TrimSpace(stdout))
	require.Zero(t, w.tasks.acks)
	require.Empty(t, w.reviews.verdicts)
}

// TestTheVerdictScenariosNeverPrintTheToken uses the canary token for every verdict.
func TestTheVerdictScenariosNeverPrintTheToken(t *testing.T) {
	for _, name := range []string{"verdict-uphold", "verdict-dismiss", "verdict-needs-human"} {
		t.Run(name, func(t *testing.T) {
			w := verdictWorld(t, upholdCase+dismissCase+needsHumanCase)
			code, stdout, stderr := playVerdictScenario(t, w, name)
			require.Equal(t, exitOK, code, "stderr: %s", stderr)
			require.NotContains(t, stdout, testToken)
			require.NotContains(t, stderr, testToken)
		})
	}
}
