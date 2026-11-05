// Package workflow models the change's life as data: the task envelope an agent receives, the workflow definition
// (its steps, their order and deadlines) and the validator that decides whether a workflow can run. An invalid
// workflow fails closed with ErrPolicyInvalid.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
//
// # Purpose
//
// TECH_SPEC 5.1 describes the workflow as YAML that becomes a Go value in this build. This package holds that value,
// the default one, and the rules of 5.1. The YAML loader is Later and must produce the same value.
//
// # API
//
//   - Workflow and Step: the value. Step.After lists prerequisites. Step.Role and Step.Output are lifted out of With so
//     the validator can check them; With holds the other arguments and is opaque here. Step.Timeout zero means none.
//   - Default(): the fourteen steps of 5.1, in the spec's order, with its deadlines and retries.
//   - Workflow.Validate(Registry): the rules of 5.1. The Registry lists the known step types, roles and schema ids.
//   - ErrPolicyInvalid: the sentinel every validation failure wraps.
//   - MaxTimeout: 24h, the largest timeout the spec gives anywhere. It is the bound for "every timeout is bounded".
//
// # Invariants
//
//   - The only cycles are retries: an after graph is a DAG, checked by a topological sort.
//   - A retry (on_fail.retry) points at an existing step that is an ancestor of the failing step.
//   - Every after names an existing step; ids are non-empty and unique; every step type, role and output schema is
//     registered; a timeout is between zero (none) and MaxTimeout.
//   - on_fail.then is escalate or reject, the two outcomes the spec names.
//
// # Failure modes
//
// Validation reports every problem, not only the first, and each message names the step. A workflow that does not
// validate must not run. Steps do no I/O here, so nothing in this package can fail at run time.
//
// # Choices the spec leaves open
//
// "Bounded" has no number in the spec, so MaxTimeout is the reject_after of the policy. A zero timeout means the
// step declares none; the spec does not say that every wait must have one, so Default does not invent one.
//
// # Tests
//
// definition_test.go checks the fourteen steps, the ordering and deadlines of 5.1, and that Default validates with the
// spec's registry; testdata/default_workflow.golden.json is the frozen wire form, generated once and checked against
// 5.1 by eye. validate_test.go has a table with one case per rule, a long-cycle case, the retry direction, and the
// timeout bound at its edge. task_test.go covers the task envelope.
package workflow
