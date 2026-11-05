package effects

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// MaxCheckSummary is the most text a check run summary may carry.
const MaxCheckSummary = change.MaxCheckSummary

// CheckPayload is the payload of a github_check effect. It is the domain type, shared with every producer (the intake
// slop check and the gate), so a producer cannot write a shape the executor refuses.
type CheckPayload = change.CheckPayload

// DecodeCheckPayload strictly decodes and validates a github_check payload.
func DecodeCheckPayload(raw json.RawMessage) (CheckPayload, error) {
	return change.DecodeCheckPayload(raw)
}

// GitHubCheckDeps are the dependencies of GitHubCheck.
type GitHubCheckDeps struct {
	Checks ports.CheckWriter
}

// GitHubCheck performs github_check effects.
type GitHubCheck struct {
	deps GitHubCheckDeps
}

// NewGitHubCheck builds the executor, or refuses when a dependency is missing.
func NewGitHubCheck(d GitHubCheckDeps) (GitHubCheck, error) {
	if d.Checks == nil {
		return GitHubCheck{}, fmt.Errorf("github_check executor: missing checks")
	}
	return GitHubCheck{deps: d}, nil
}

// Execute publishes the check run named by the effect and keys it with the effect key.
func (g GitHubCheck) Execute(ctx context.Context, fx change.Effect) error {
	if fx.Kind != change.EffectGitHubCheck {
		return failf(nil, "github_check executor: effect %s has kind %s", fx.Key, fx.Kind)
	}
	p, err := DecodeCheckPayload(fx.Payload)
	if err != nil {
		return failf(err, "github_check effect %s: payload refused: %v", fx.Key, err)
	}
	if fx.Target != string(p.Repo) {
		return failf(nil, "github_check effect %s: target %q is not repo %s", fx.Key, fx.Target, p.Repo)
	}
	c := ports.Check{
		Name:       p.Name,
		Status:     ports.CheckStatus(p.Status),
		Conclusion: p.Conclusion,
		ExternalID: string(fx.Key),
		Summary:    p.Summary,
	}
	if err := g.deps.Checks.SetCheck(ctx, p.Repo, p.SHA, c); err != nil {
		return failf(err, "github_check %s: publish check", fx.Key)
	}
	return nil
}
