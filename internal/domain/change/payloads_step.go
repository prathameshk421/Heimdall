package change

// maxRetryReasons bounds the structured reasons a retry carries (TECH_SPEC 5.1, on_fail).
const maxRetryReasons = 8

// StepRetried is the payload of step.retried: a step failed its check, and its on_fail retry target is open again.
// Step is the workflow step id of the retry target. Attempt is the attempt the retry starts, so the first retry is 2.
// Reasons are the structured reasons of the failed check, which the retry target reads from the view.
type StepRetried struct {
	Step    string   `json:"step"`
	Attempt int      `json:"attempt"`
	Reasons []string `json:"reasons"`
}

// Validate returns every problem with the payload.
func (p StepRetried) Validate() error {
	var v problems
	v.add("step", checkText(p.Step))
	v.require("attempt", p.Attempt >= 2, "must be 2 or more")
	v.require("reasons", len(p.Reasons) <= maxRetryReasons, "must hold at most 8 items")
	v.each("reasons", p.Reasons, checkText)
	return v.join()
}

// StepEscalated is the payload of step.escalated: a step failed its check after its budget of attempts was used up,
// so the change waits for a human. Step is the workflow step id that failed. Attempts counts the attempts made. Reason
// says why the change stopped, in one line.
type StepEscalated struct {
	Step     string `json:"step"`
	Attempts int    `json:"attempts"`
	Reason   string `json:"reason"`
}

// StepResumed is the payload of step.resumed: a person released an escalated step (A10 item 5). Step is the workflow
// step id. By is the person who resumed it. The step's view starts again at this event, and the step is offered again.
type StepResumed struct {
	Step string  `json:"step"`
	By   ActorID `json:"by"`
}

// Validate returns every problem with the payload.
func (p StepResumed) Validate() error {
	var v problems
	v.add("step", checkText(p.Step))
	v.add("by", checkActor(p.By))
	return v.join()
}

// Validate returns every problem with the payload.
func (p StepEscalated) Validate() error {
	var v problems
	v.add("step", checkText(p.Step))
	v.require("attempts", p.Attempts >= 1, "must be 1 or more")
	v.add("reason", checkText(p.Reason))
	return v.join()
}
