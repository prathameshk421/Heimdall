package finding

import (
	"fmt"
	"strings"
)

// Lead is a new tool result the seat must answer (REVIEW_ARCH 4.5, 5.4). The brief carries it under lead_id.
type Lead struct {
	LeadID  string `json:"lead_id"`
	Check   string `json:"check"`
	Lens    string `json:"lens"`
	Rule    string `json:"rule"`
	Level   string `json:"level"`
	File    string `json:"file"`
	Lines   [2]int `json:"lines"`
	Message string `json:"message"`
}

// Brief is what the server gave the seat: the card names it must answer and the leads it must answer.
type Brief struct {
	Obligations []string `json:"obligations"`
	Leads       []Lead   `json:"leads"`
}

// Lead verdicts (Review@2 lead_checks).
const (
	LeadConfirmed = "confirmed"
	LeadDropped   = "dropped"
)

// LeadCheck is the answer to one lead: confirmed (a finding must cite its run) or dropped (a reason is required).
type LeadCheck struct {
	LeadID  string `json:"lead_id"`
	Verdict string `json:"verdict"`
	Reason  string `json:"reason,omitempty"`
}

// SubmitV2 is a review as a seat submits it under Review@2: Review@1 plus lead_checks (REVIEW_ARCH section 11).
type SubmitV2 struct {
	Review
	LeadChecks []LeadCheck `json:"lead_checks"`
}

// Refusal says what is wrong with a submission and the smallest valid replacement (AGENT_INTERACTION_ARCH 6.2).
// An empty Example means the refusal has no example.
type Refusal struct {
	Field   string `json:"field"`
	Problem string `json:"problem"`
	Example string `json:"example,omitempty"`
}

// CheckSubmit applies the submit rules of REVIEW_ARCH 5.5 and returns every refusal, in rule order. An empty result
// accepts the submission.
//
//  1. Every obligation has one rule check, and every lead has one lead check.
//  2. A card marked holds is not cited by any finding; a card marked violated is cited by one.
//  3. A confirmed lead is cited by a finding with a tool_result of its check on its file; a dropped lead has a reason.
//  4. clean is true only when there are no findings.
func CheckSubmit(b Brief, s SubmitV2) []Refusal {
	var out []Refusal
	out = append(out, checkObligations(b, s)...)
	out = append(out, checkLeadsAnswered(b, s)...)
	out = append(out, checkRuleVerdicts(s)...)
	out = append(out, checkLeadVerdicts(b, s)...)
	if s.Clean && len(s.Findings) > 0 {
		out = append(out, Refusal{"clean",
			fmt.Sprintf("clean is true but findings has %s", entries(len(s.Findings))), "\"clean\": false"})
	}
	return out
}

func checkObligations(b Brief, s SubmitV2) []Refusal {
	var out []Refusal
	for _, card := range b.Obligations {
		if !hasRuleCheck(s, card) {
			out = append(out, Refusal{"rule_checks",
				fmt.Sprintf("card %s is in the brief's obligations and has no entry", card),
				fmt.Sprintf(`{"card": %q, "verdict": "holds", "note": "why the change keeps this rule"}`, card)})
		}
	}
	return out
}

func checkLeadsAnswered(b Brief, s SubmitV2) []Refusal {
	var out []Refusal
	for _, l := range b.Leads {
		if !hasLeadCheck(s, l.LeadID) {
			out = append(out, Refusal{"lead_checks",
				fmt.Sprintf("lead %s is in the brief's leads and has no entry", l.LeadID),
				fmt.Sprintf(`{"lead_id": %q, "verdict": "dropped", "reason": "why this is not a real problem"}`, l.LeadID)})
		}
	}
	return out
}

func checkRuleVerdicts(s SubmitV2) []Refusal {
	var out []Refusal
	for i, rc := range s.RuleChecks {
		cited := citingFinding(s, rc.Card)
		switch {
		case rc.Verdict == RuleHolds && cited >= 0:
			out = append(out, Refusal{fmt.Sprintf("rule_checks[%d].verdict", i),
				fmt.Sprintf("card %s is marked holds but findings[%d] cites it", rc.Card, cited),
				fmt.Sprintf(`{"card": %q, "verdict": "violated", "note": "what the change breaks"}`, rc.Card)})
		case rc.Verdict == RuleViolated && cited < 0:
			out = append(out, Refusal{fmt.Sprintf("rule_checks[%d].verdict", i),
				fmt.Sprintf("card %s is marked violated and no finding cites it", rc.Card),
				fmt.Sprintf(`add a finding with "context_refs": [%q], or mark the card holds`, rc.Card)})
		}
	}
	return out
}

func checkLeadVerdicts(b Brief, s SubmitV2) []Refusal {
	var out []Refusal
	for i, lc := range s.LeadChecks {
		switch lc.Verdict {
		case LeadConfirmed:
			if l, ok := leadByID(b, lc.LeadID); ok && !runCited(s, l) {
				out = append(out, Refusal{fmt.Sprintf("lead_checks[%d].verdict", i),
					fmt.Sprintf("lead %s is marked confirmed and no finding cites its run", lc.LeadID), ""})
			}
		case LeadDropped:
			if strings.TrimSpace(lc.Reason) == "" {
				out = append(out, Refusal{fmt.Sprintf("lead_checks[%d].reason", i),
					fmt.Sprintf("lead %s is marked dropped and has no reason", lc.LeadID),
					fmt.Sprintf(`{"lead_id": %q, "verdict": "dropped", "reason": "why this is not a real problem"}`, lc.LeadID)})
			}
		}
	}
	return out
}

func hasRuleCheck(s SubmitV2, card string) bool {
	for _, rc := range s.RuleChecks {
		if rc.Card == card {
			return true
		}
	}
	return false
}

func hasLeadCheck(s SubmitV2, id string) bool {
	for _, lc := range s.LeadChecks {
		if lc.LeadID == id {
			return true
		}
	}
	return false
}

func leadByID(b Brief, id string) (Lead, bool) {
	for _, l := range b.Leads {
		if l.LeadID == id {
			return l, true
		}
	}
	return Lead{}, false
}

// citingFinding returns the index of the first finding whose context_refs names card, or -1.
func citingFinding(s SubmitV2, card string) int {
	for i, f := range s.Findings {
		for _, ref := range f.ContextRefs {
			if ref == card {
				return i
			}
		}
	}
	return -1
}

// runCited reports whether a finding carries a tool_result of the lead's check on the lead's file: the finding cites
// the run that produced the lead.
func runCited(s SubmitV2, l Lead) bool {
	for _, f := range s.Findings {
		for _, e := range f.Evidence {
			if e.Type == EvidenceToolResult && e.ToolResult != nil &&
				e.ToolResult.CheckID == l.Check && e.ToolResult.Location.File == l.File {
				return true
			}
		}
	}
	return false
}
