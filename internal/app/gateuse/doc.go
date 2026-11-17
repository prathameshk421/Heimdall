// Package gateuse reads the facts the merge rule needs that the event log does not hold: the policy at the merge base
// and at head, the changed files, the test tree at head and the policy owner's approval (change.Outside). The rule itself
// is change.EvaluateGate on the view, and the gate row of the reconciler publishes its result (HARNESS_DESIGN B5).
//
// # API
//
//   - NewSource(SourceConfig) (*RepoSource, error): reads pull request data and git objects through ports.
//   - RepoSource.Outside(ctx, Request) (change.Outside, error): the facts for one pull request head.
//
// # Failure modes
//
// A pull request, file or policy that cannot be read is an error naming it; the reconciler then stops that Run and the
// next sweep tries again. A policy that does not decode is "not known", and the gate is red while it is unknown.
package gateuse
