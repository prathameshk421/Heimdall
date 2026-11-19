package effects

import (
	"context"
	"fmt"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/sarif"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// checkTimeout bounds one profile check run.
const checkTimeout = 5 * time.Minute

// ChecksDeps are the dependencies of Checks.
type ChecksDeps struct {
	Source   Source
	Sandbox  ports.Sandbox
	Profiles ports.ProfileStore
	Ingest   Ingest
	IDs      ports.IDs
	Clock    ports.Clock
}

// Checks performs run_checks effects (HARNESS_DESIGN B7): it runs each named check of the repository's promoted profile
// on the pull request head, and on the base too when the check compares against it, and records checks.reported with the
// results the head added. A check that cannot run is an entry of errors; it never fails the job, because a broken check
// must not stall the review.
type Checks struct {
	deps ChecksDeps
}

// NewChecks builds the executor, or refuses when a dependency is missing.
func NewChecks(d ChecksDeps) (Checks, error) {
	if d.Source == nil || d.Sandbox == nil || d.Profiles == nil || d.Ingest == nil || d.IDs == nil || d.Clock == nil {
		return Checks{}, fmt.Errorf("run_checks executor: missing source, sandbox, profiles, ingest, ids or clock")
	}
	return Checks{deps: d}, nil
}

// Execute runs the checks and records the report.
func (c Checks) Execute(ctx context.Context, fx change.Effect) error {
	if fx.Kind != change.EffectRunChecks {
		return failf(nil, "run_checks executor: effect %s has kind %s", fx.Key, fx.Kind)
	}
	p, err := change.DecodeRunChecksPayload(fx.Payload)
	if err != nil {
		return failf(err, "run_checks effect %s: payload refused: %v", fx.Key, err)
	}
	profile, ok, err := c.deps.Profiles.ActiveProfile(ctx, string(p.Repo))
	if err != nil {
		return failf(err, "run_checks %s: read the profile", p.Change)
	}
	if !ok {
		return failf(nil, "run_checks %s: the repository has no promoted profile", p.Change)
	}
	if err := c.deps.Source.Fetch(ctx, p.Repo, p.Head); err != nil {
		return failf(err, "run_checks %s: fetch head source", p.Change)
	}
	report := change.ChecksReported{Head: p.Head, Results: []sarif.Result{}, Errors: []change.CheckError{}}
	var all []sarif.Result
	for _, checkID := range p.CheckIDs {
		row, found := checkRow(profile.Spec.Checks, checkID)
		if !found {
			report.Errors = append(report.Errors, change.CheckError{Check: checkID, Reason: "the profile has no such check"})
			continue
		}
		results, err := c.runOne(ctx, p, row)
		if err != nil {
			report.Errors = append(report.Errors, change.CheckError{Check: checkID, Reason: err.Error()})
			continue
		}
		all = append(all, results...)
	}
	if len(all) > change.MaxCheckResults {
		report.More = len(all) - change.MaxCheckResults
		all = all[:change.MaxCheckResults]
	}
	report.Results = append(report.Results, all...)
	ev, err := newFact(c.deps.IDs, c.deps.Clock, change.EventChecksReported, change.Actor{Kind: change.ActorSystem, ID: "heimdall"}, report)
	if err != nil {
		return failf(err, "run_checks %s: build event", p.Change)
	}
	if err := c.deps.Ingest.Ingest(ctx, p.Change, []change.Event{ev}); err != nil {
		return failf(err, "run_checks %s: ingest checks.reported", p.Change)
	}
	return nil
}

func checkRow(rows []ports.ProfileCheck, id string) (ports.ProfileCheck, bool) {
	for _, r := range rows {
		if r.ID == id {
			return r, true
		}
	}
	return ports.ProfileCheck{}, false
}

// runOne runs one check on the head and, with a baseline, on the base, and returns what the head added.
func (c Checks) runOne(ctx context.Context, p change.RunChecksPayload, row ports.ProfileCheck) ([]sarif.Result, error) {
	head, err := c.runAt(ctx, p, row, p.Head, "")
	if err != nil {
		return nil, err
	}
	if !row.Baseline || p.Base == "" {
		return head, nil
	}
	if err := c.deps.Source.Fetch(ctx, p.Repo, p.Base); err != nil {
		return nil, fmt.Errorf("fetch base source: %w", ports.ErrUnavailable)
	}
	base, err := c.runAt(ctx, p, row, p.Base, p.Head)
	if err != nil {
		return nil, err
	}
	return sarif.New(base, head), nil
}

func (c Checks) runAt(ctx context.Context, p change.RunChecksPayload, row ports.ProfileCheck, sha, head id.SHA) ([]sarif.Result, error) {
	res, err := c.deps.Sandbox.Run(ctx, ports.SandboxJob{Kind: ports.SandboxCheck, CheckID: row.ID, Repo: p.Repo, SHA: sha,
		Change: p.Change, Head: head, Timeout: checkTimeout})
	if err != nil {
		return nil, fmt.Errorf("the check did not run: %w", ports.ErrUnavailable)
	}
	results, err := sarif.Parse(res.Sarif, row.ID, row.Lens)
	if err != nil {
		return nil, fmt.Errorf("the check wrote no valid SARIF")
	}
	return results, nil
}
