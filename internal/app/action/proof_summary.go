package action

import (
	"fmt"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// StepSummaryIn is what the proof job's own summary shows. Every value is the run's own: no model text, no comment and
// no path, so the Markdown holds nothing a pull request wrote.
type StepSummaryIn struct {
	Number  int
	Outcome proof.Outcome
	// Stop is how the agent loop ended, or "" when no model ran. Turns and CostUSD are the run's own.
	Stop    ports.AgentStop
	Turns   int
	CostUSD float64
	// Action is the action area at the merge base: its mode and proof_required decide the check.
	Action policy.Action
}

// greenIsNotAPass is the line every summary carries. The proof job ends green whenever it wrote a result, by design:
// the gate decides, from that result.
const greenIsNotAPass = "Green means this job ran and wrote its result. It does not mean the change passed."

// ProofStepSummary is the Markdown the proof step writes to the job summary: the outcome, its reason, the turns and
// the cost. In shadow mode it says whether enforce mode would block and why; in enforce mode it names the check
// conclusion the gate posts.
func ProofStepSummary(in StepSummaryIn) string {
	o := in.Outcome
	act := in.Action.Effective()
	var b strings.Builder
	fmt.Fprintf(&b, "### Heimdall proof for #%d: %s\n\n", in.Number, o.Kind)
	fmt.Fprintf(&b, "**%s** The gate reads the result and posts the `heimdall/proof` check.\n\n", greenIsNotAPass)
	fmt.Fprintf(&b, "- Outcome: %s\n", o.Headline())
	if o.Reason != "" {
		fmt.Fprintf(&b, "- Reason: `%s`\n", o.Reason)
	}
	if o.Note != "" {
		fmt.Fprintf(&b, "- Note: `%s`\n", o.Note)
	}
	if in.Stop != "" {
		fmt.Fprintf(&b, "- Turns: %d (the agent loop stopped: `%s`)\n", in.Turns, in.Stop)
	} else {
		b.WriteString("- Turns: 0 (no model call)\n")
	}
	fmt.Fprintf(&b, "- Cost: $%.4f\n", in.CostUSD)
	fmt.Fprintf(&b, "- Candidates: %d in, %d proven, %d blocking\n", o.Counts.In, o.Counts.Proven, o.Counts.Blocking)
	fmt.Fprintf(&b, "- Mode: %s\n", act.Mode)
	b.WriteString(checkLine(o, act) + "\n")
	return b.String()
}

// checkLine says what the result means for the heimdall/proof check. Shadow mode posts neutral, so the line says what
// enforce mode, with the same proof_required, would do.
func checkLine(o proof.Outcome, act policy.Action) string {
	if act.Mode == policy.ActionModeShadow {
		would := o.Conclusion(policy.Action{Mode: policy.ActionModeEnforce, ProofRequired: act.ProofRequired})
		switch would {
		case proof.ConclusionFailure, proof.ConclusionActionRequired:
			return fmt.Sprintf("- Would block because: %s (enforce mode would post `%s`).", o.Headline(), would)
		case proof.ConclusionSuccess:
			return "- Would pass in enforce mode."
		case proof.ConclusionNeutral:
		}
		return "- Would not block in enforce mode: `heimdall/proof` is advisory unless `action.proof_required` is set."
	}
	return fmt.Sprintf("- Check: the gate posts `heimdall/proof` as `%s`.", o.Conclusion(act))
}
