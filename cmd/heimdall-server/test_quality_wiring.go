package main

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/policyyaml"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testset"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const targetModulePath = "github.com/prathameshk421/flip_the_switch_hackathon"

func testQualityForbiddenImports() []string {
	return []string{targetModulePath + "/internal"}
}

func needsTestSignoff(pol policy.Policy, expectedPaths []string) bool {
	for _, expected := range expectedPaths {
		for _, sensitive := range pol.Tests.Sensitive {
			if policy.MatchGlob(sensitive, expected) {
				return true
			}
		}
	}
	return false
}

// testQualityNeedsSignoff reads the policy at the branch base and the submission blob. The callback cannot return an
// error, so an unreadable or invalid policy is treated conservatively as requiring sign-off. A missing policy is not
// sensitive by definition; the gate still fails closed when it later evaluates the repository policy.
func testQualityNeedsSignoff(ctx context.Context, blobs ports.BlobStore, vcs ports.CodeReader) func([]change.Event) bool {
	return func(events []change.Event) bool {
		trig, base, ref, ok := testQualityInputs(events)
		if !ok {
			return false
		}
		raw, err := vcs.ReadFile(ctx, trig.Repo, base, policyyaml.File)
		if err != nil {
			return !errors.Is(err, ports.ErrNotFound)
		}
		pol, err := policyyaml.Decode(raw)
		if err != nil {
			return true
		}
		rc, err := blobs.Get(ctx, ref)
		if err != nil {
			return true
		}
		defer func() { _ = rc.Close() }()
		var sub testset.Submission
		if err := json.NewDecoder(rc).Decode(&sub); err != nil {
			return true
		}
		return needsTestSignoff(pol, sub.ExpectedPaths)
	}
}

func testQualityInputs(events []change.Event) (change.Triggered, id.SHA, workflow.BlobRef, bool) {
	var trig change.Triggered
	var base id.SHA
	var ref workflow.BlobRef
	for _, ev := range events {
		payload, err := change.Decode(ev)
		if err != nil {
			continue
		}
		switch p := payload.(type) {
		case change.Triggered:
			trig = p
		case change.BranchEnsured:
			base = p.BaseSHA
		case change.TestsSubmitted:
			if len(ev.BlobRefs) > 0 {
				ref = ev.BlobRefs[0]
			}
		}
	}
	return trig, base, ref, trig.Repo != "" && base != "" && ref != ""
}
