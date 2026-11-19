package main

import (
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/effects"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/outbox"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// reviewExecutors builds the executors of the review area: verifying the evidence of findings.
func reviewExecutors(ad adapters, profiles ports.ProfileStore, sandbox profileSandbox, in effects.Ingest, gen ports.IDs, clk ports.Clock, recheck effects.MemoryRecheck) (map[change.EffectKind]outbox.Executor, error) {
	verifyEvidence, err := effects.NewVerifyEvidence(effects.VerifyEvidenceDeps{
		Source: vcsSource{vcs: ad.vcs}, Sandbox: sandbox, Profiles: profiles, Memory: recheck, Ingest: in, IDs: gen, Clock: clk,
	})
	if err != nil {
		return nil, fmt.Errorf("build verify_evidence executor: %w", err)
	}
	runChecks, err := effects.NewChecks(effects.ChecksDeps{
		Source: vcsSource{vcs: ad.vcs}, Sandbox: sandbox, Profiles: profiles, Ingest: in, IDs: gen, Clock: clk,
	})
	if err != nil {
		return nil, fmt.Errorf("build run_checks executor: %w", err)
	}
	return map[change.EffectKind]outbox.Executor{
		change.EffectVerifyEvidence: verifyEvidence,
		change.EffectRunChecks:      runChecks,
	}, nil
}
