package effects

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// acceptanceDir holds every ticket's Heimdall tests. The acceptance run covers all of it, so earlier tickets' tests
// guard against regressions (D150).
const acceptanceDir = "tests/heimdall"

// AcceptanceDeps are the dependencies of Acceptance.
type AcceptanceDeps struct {
	Source  Source
	Sandbox ports.Sandbox
	Checks  ports.CheckWriter
}

// Acceptance performs acceptance effects: it runs every Heimdall test on a pull request head in the sandbox and
// reports the result as the heimdall/acceptance check, which the merge rule requires (TECH_SPEC 12.4).
type Acceptance struct {
	deps AcceptanceDeps
}

// NewAcceptance builds the executor, or refuses when a dependency is missing.
func NewAcceptance(d AcceptanceDeps) (Acceptance, error) {
	if d.Source == nil || d.Sandbox == nil || d.Checks == nil {
		return Acceptance{}, fmt.Errorf("acceptance executor: missing source, sandbox or checks")
	}
	return Acceptance{deps: d}, nil
}

// Execute marks the check in progress, runs the tests and completes the check. A sandbox failure leaves the check in
// progress and returns the error, so the outbox retries the run.
func (a Acceptance) Execute(ctx context.Context, fx change.Effect) error {
	if fx.Kind != change.EffectAcceptance {
		return failf(nil, "acceptance executor: effect %s has kind %s", fx.Key, fx.Kind)
	}
	p, err := DecodeCheckPayload(fx.Payload)
	if err != nil {
		return failf(err, "acceptance effect %s: payload refused: %v", fx.Key, err)
	}
	if fx.Target != string(p.Repo) {
		return failf(nil, "acceptance effect %s: target %q is not repo %s", fx.Key, fx.Target, p.Repo)
	}
	check := ports.Check{Name: p.Name, Status: ports.CheckInProgress, ExternalID: string(fx.Key),
		Summary: "Heimdall is running every acceptance test on this head."}
	if err := a.deps.Checks.SetCheck(ctx, p.Repo, p.SHA, check); err != nil {
		return failf(err, "acceptance %s: mark check in progress", fx.Key)
	}
	if err := a.deps.Source.Fetch(ctx, p.Repo, p.SHA); err != nil {
		return failf(err, "acceptance %s: fetch head source", fx.Key)
	}
	res, err := a.deps.Sandbox.Run(ctx, ports.SandboxJob{
		Kind: ports.SandboxRedFirst, Repo: p.Repo, SHA: p.SHA, Dir: acceptanceDir, Timeout: 5 * time.Minute,
	})
	if err != nil {
		return failf(err, "acceptance %s: sandbox run", fx.Key)
	}
	check.Status = ports.CheckCompleted
	check.Conclusion, check.Summary = acceptanceVerdict(res)
	if err := a.deps.Checks.SetCheck(ctx, p.Repo, p.SHA, check); err != nil {
		return failf(err, "acceptance %s: complete check", fx.Key)
	}
	return nil
}

// acceptanceVerdict is success only when at least one test ran and every test passed. The summary names the tests
// that did not pass.
func acceptanceVerdict(res ports.SandboxResult) (change.CIConclusion, string) {
	var bad []string
	for _, t := range res.Tests {
		if t.Outcome != ports.OutcomePassed && t.Outcome != ports.OutcomeSkipped {
			bad = append(bad, t.Name)
		}
	}
	switch {
	case len(res.Tests) == 0:
		return change.CIFailure, "No Heimdall tests ran on this head."
	case len(bad) == 0:
		return change.CISuccess, fmt.Sprintf("All %d Heimdall acceptance tests pass.", len(res.Tests))
	default:
		summary := fmt.Sprintf("%d of %d Heimdall acceptance tests do not pass: %s.", len(bad), len(res.Tests), strings.Join(bad, ", "))
		return change.CIFailure, clipText(summary, MaxCheckSummary)
	}
}

// clipText cuts s to at most limit bytes.
func clipText(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return strings.ToValidUTF8(s[:limit], "")
}
