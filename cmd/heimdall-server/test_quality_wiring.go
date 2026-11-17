package main

import (
	"context"
	"errors"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/policyyaml"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testset"
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

// testQualityNeedsSignoff reads the policy at the branch base. The callback cannot return an error, so an unreadable or
// invalid policy is treated conservatively as requiring sign-off. A missing policy is not sensitive by definition; the
// gate still fails closed when it later evaluates the repository policy.
func testQualityNeedsSignoff(vcs ports.CodeReader) func(context.Context, change.Repo, id.SHA, testset.Submission) bool {
	return func(ctx context.Context, repo change.Repo, base id.SHA, sub testset.Submission) bool {
		raw, err := vcs.ReadFile(ctx, repo, base, policyyaml.File)
		if err != nil {
			return !errors.Is(err, ports.ErrNotFound)
		}
		pol, err := policyyaml.Decode(raw)
		if err != nil {
			return true
		}
		return needsTestSignoff(pol, sub.ExpectedPaths)
	}
}
