package main

import (
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/effects"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/outbox"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// testsExecutors builds the executors of the tests area: committing the tests, their quality checks and the
// acceptance run.
func testsExecutors(ad adapters, blobs ports.BlobStore, sandbox profileSandbox, in effects.Ingest, gen ports.IDs, clk ports.Clock) (map[change.EffectKind]outbox.Executor, error) {
	commitTests, err := effects.NewCommitTests(effects.CommitTestsDeps{
		Blobs: blobs, Branches: ad.vcs, Ingest: in, IDs: gen, Clock: clk, Code: ad.vcs,
	})
	if err != nil {
		return nil, fmt.Errorf("build github_commit_tests executor: %w", err)
	}
	testQuality, err := effects.NewTestQuality(effects.TestQualityDeps{
		Blobs: blobs, Source: vcsSource{vcs: ad.vcs}, Sandbox: sandbox, Ingest: in, IDs: gen, Clock: clk, Code: ad.vcs,
		ForbiddenImports: testQualityForbiddenImports(), NeedsSignoff: testQualityNeedsSignoff(ad.vcs),
	})
	if err != nil {
		return nil, fmt.Errorf("build test_quality executor: %w", err)
	}
	acceptance, err := effects.NewAcceptance(effects.AcceptanceDeps{Source: vcsSource{vcs: ad.vcs}, Sandbox: sandbox, Checks: ad.vcs})
	if err != nil {
		return nil, fmt.Errorf("build acceptance executor: %w", err)
	}
	return map[change.EffectKind]outbox.Executor{
		change.EffectAcceptance:        acceptance,
		change.EffectGitHubCommitTests: commitTests,
		change.EffectTestQuality:       testQuality,
	}, nil
}
